package mesh

import (
	"container/list"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/pending_txs"
	"math/big"
	"sync"
)

type layerMutex struct {
	m            sync.Mutex
	layerWorkers uint32
}

type MeshDB struct {
	log.Log
	blockCache         blockCache
	layers             database.DB
	blocks             database.DB
	transactions       database.Database
	contextualValidity database.DB //map blockId to contextualValidation state of block
	patterns           database.DB //map blockId to contextualValidation state of block
	meshTxs            database.Database
	meshTxsMutex       sync.Mutex
	orphanBlocks       map[types.LayerID]map[types.BlockID]struct{}
	layerMutex         map[types.LayerID]*layerMutex
	lhMutex            sync.Mutex
}

func NewPersistentMeshDB(path string, log log.Log) (*MeshDB, error) {
	bdb := database.NewLevelDbStore(path+"blocks", nil, nil)
	ldb := database.NewLevelDbStore(path+"layers", nil, nil)
	vdb := database.NewLevelDbStore(path+"validity", nil, nil)
	pdb := database.NewLevelDbStore(path+"patterns", nil, nil)
	tdb, err := database.NewLDBDatabase(path+"transactions", 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize transactions db: %v", err)
	}
	mtx, err := database.NewLDBDatabase(path+"meshTxs", 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize mesh transactions db: %v", err)
	}

	ll := &MeshDB{
		Log:                log,
		blockCache:         NewBlockCache(100 * layerSize),
		blocks:             bdb,
		layers:             ldb,
		transactions:       tdb,
		patterns:           pdb,
		contextualValidity: vdb,
		meshTxs:            mtx,
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
	}
	return ll, nil
}

func NewMemMeshDB(log log.Log) *MeshDB {
	ll := &MeshDB{
		Log:                log,
		blockCache:         NewBlockCache(100 * layerSize),
		blocks:             database.NewMemDatabase(),
		layers:             database.NewMemDatabase(),
		contextualValidity: database.NewMemDatabase(),
		transactions:       database.NewMemDatabase(),
		patterns:           database.NewMemDatabase(),
		meshTxs:            database.NewMemDatabase(),
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
	}
	return ll
}

func (m *MeshDB) Close() {
	m.blocks.Close()
	m.layers.Close()
	m.transactions.Close()
	m.contextualValidity.Close()
}

var ErrAlreadyExist = errors.New("block already exist in database")

func (m *MeshDB) AddBlock(bl *types.Block) error {
	if _, err := m.getMiniBlockBytes(bl.ID()); err == nil {
		m.With().Warning("Block already exist in database", log.BlockId(uint64(bl.Id)))
		return ErrAlreadyExist
	}
	if err := m.writeBlock(bl); err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) GetBlock(id types.BlockID) (*types.Block, error) {

	if blkh := m.blockCache.Get(id); blkh != nil {
		return blkh, nil
	}

	b, err := m.getMiniBlockBytes(id)
	if err != nil {
		return nil, err
	}
	mbk := &types.Block{}
	err = types.BytesToInterface(b, mbk)
	return mbk, err
}

func (m *MeshDB) LayerBlocks(index types.LayerID) ([]*types.Block, error) {
	ids, err := m.layerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blocks := make([]*types.Block, 0, len(ids))
	for _, k := range ids {
		block, err := m.GetBlock(k)
		if err != nil {
			return nil, errors.New("could not retrieve block " + fmt.Sprint(k) + " " + err.Error())
		}
		blocks = append(blocks, block)
	}

	return blocks, nil

}

func (m *MeshDB) LayerBlockIds(index types.LayerID) ([]types.BlockID, error) {

	idSet, err := m.layerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blockids := make([]types.BlockID, 0, len(idSet))
	for _, k := range idSet {
		blockids = append(blockids, k)
	}

	return blockids, nil
}

// The block handler func should return two values - a bool indicating whether or not we should stop traversing after the current block (happy flow)
// and an error indicating that an error occurred while handling the block, the traversing will stop in that case as well (error flow)
func (m *MeshDB) ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error {
	blocksToVisit := list.New()
	for id := range view {
		blocksToVisit.PushBack(id)
	}
	seenBlocks := make(map[types.BlockID]struct{})
	for blocksToVisit.Len() > 0 {
		block, err := m.GetBlock(blocksToVisit.Remove(blocksToVisit.Front()).(types.BlockID))
		if err != nil {
			return err
		}

		//catch blocks that were referenced after more than one layer, and slipped through the stop condition
		if block.LayerIndex < layer {
			continue
		}

		//execute handler
		stop, err := blockHandler(block)
		if err != nil {
			return err
		}

		if stop {
			m.Log.With().Debug("ForBlockInView stopped", log.BlockId(uint64(block.ID())))
			break
		}

		//stop condition: referenced blocks must be in lower layers, so we don't traverse them
		if block.LayerIndex == layer {
			continue
		}

		//push children to bfs queue
		for _, id := range block.ViewEdges {
			if _, found := seenBlocks[id]; !found {
				seenBlocks[id] = struct{}{}
				blocksToVisit.PushBack(id)
			}
		}
	}
	return nil
}

func (m *MeshDB) layerBlockIds(index types.LayerID) ([]types.BlockID, error) {

	ids, err := m.layers.Get(index.ToBytes())
	if err != nil {
		return nil, fmt.Errorf("error getting layer %v from database %v", index, err)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids for layer %v in database ", index)
	}

	idSet, err := types.BytesToBlockIds(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	return idSet, nil
}

func (m *MeshDB) getMiniBlockBytes(id types.BlockID) ([]byte, error) {
	return m.blocks.Get(id.ToBytes())
}

func (m *MeshDB) ContextualValidity(id types.BlockID) (bool, error) {
	b, err := m.contextualValidity.Get(id.ToBytes())
	if err != nil {
		return false, err
	}
	return b[0] == 1, nil //bytes to bool
}

func (m *MeshDB) SaveContextualValidity(id types.BlockID, valid bool) error {
	var v []byte
	if valid {
		v = TRUE
	} else {
		v = FALSE
	}
	err := m.contextualValidity.Put(id.ToBytes(), v)
	if err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) writeBlock(bl *types.Block) error {
	bytes, err := types.InterfaceToBytes(bl)
	if err != nil {
		return fmt.Errorf("could not encode bl")
	}

	if err := m.blocks.Put(bl.ID().ToBytes(), bytes); err != nil {
		return fmt.Errorf("could not add bl to %v databacse %v", bl.ID(), err)
	}

	m.updateLayerWithBlock(&bl.MiniBlock)

	m.blockCache.put(bl)

	return nil
}

func (m *MeshDB) updateLayerWithBlock(blk *types.MiniBlock) error {
	lm := m.getLayerMutex(blk.LayerIndex)
	defer m.endLayerWorker(blk.LayerIndex)
	lm.m.Lock()
	defer lm.m.Unlock()
	ids, err := m.layers.Get(blk.LayerIndex.ToBytes())
	var blockIds []types.BlockID
	if err != nil {
		//layer doesnt exist, need to insert new layer
		blockIds = make([]types.BlockID, 0, 1)
	} else {
		blockIds, err = types.BytesToBlockIds(ids)
		if err != nil {
			return errors.New("could not get all blocks from database ")
		}
	}
	m.Debug("added block %v to layer %v", blk.ID(), blk.LayerIndex)
	blockIds = append(blockIds, blk.ID())
	w, err := types.BlockIdsAsBytes(blockIds)
	if err != nil {
		return errors.New("could not encode layer blk ids")
	}
	m.layers.Put(blk.LayerIndex.ToBytes(), w)
	return nil
}

//try delete layer Handler (deletes if pending pendingCount is 0)
func (m *MeshDB) endLayerWorker(index types.LayerID) {
	m.lhMutex.Lock()
	defer m.lhMutex.Unlock()

	ll, found := m.layerMutex[index]
	if !found {
		panic("trying to double close layer mutex")
	}

	ll.layerWorkers--
	if ll.layerWorkers == 0 {
		delete(m.layerMutex, index)
	}
}

//returns the existing layer Handler (crates one if doesn't exist)
func (m *MeshDB) getLayerMutex(index types.LayerID) *layerMutex {
	m.lhMutex.Lock()
	defer m.lhMutex.Unlock()
	ll, found := m.layerMutex[index]
	if !found {
		ll = &layerMutex{}
		m.layerMutex[index] = ll
	}
	ll.layerWorkers++
	return ll
}

func (m *MeshDB) writeTransactions(txs []*types.AddressableSignedTransaction) error {
	batch := m.transactions.NewBatch()
	for _, t := range txs {
		bytes, err := types.AddressableTransactionAsBytes(t)
		if err != nil {
			m.Error("could not marshall tx %v to bytes ", err)
			return err
		}
		id := types.GetTransactionId(t.SerializableSignedTransaction)
		if err := batch.Put(id[:], bytes); err != nil {
			m.Error("could not write tx %v to database ", hex.EncodeToString(id[:]), err)
			return err
		}
		m.Debug("write tx %v to db", hex.EncodeToString(id[:]))
	}
	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transactions: %v", err)
	}
	return nil
}

func Transaction2SerializableTransaction(tx *Transaction) *types.AddressableSignedTransaction {
	inner := types.InnerSerializableSignedTransaction{
		AccountNonce: tx.AccountNonce,
		Recipient:    *tx.Recipient,
		Amount:       tx.Amount.Uint64(),
		GasLimit:     tx.GasLimit,
		GasPrice:     tx.GasPrice.Uint64(),
	}
	sst := &types.SerializableSignedTransaction{
		InnerSerializableSignedTransaction: inner,
	}
	return &types.AddressableSignedTransaction{
		SerializableSignedTransaction: sst,
		Address:                       tx.Origin,
	}
}

func txToTiny(tx *Transaction) types.TinyTx {
	id := types.GetTransactionId(Transaction2SerializableTransaction(tx).SerializableSignedTransaction)
	return types.TinyTx{
		Id:          id,
		Origin:      tx.Origin,
		Nonce:       tx.AccountNonce,
		TotalAmount: tx.Amount.Uint64() + tx.GasPrice.Uint64(), // TODO: GasPrice represents the absolute fee here, as a temporarily hack
	}
}

func (m *MeshDB) addToMeshTxs(txs []*types.AddressableSignedTransaction, layer types.LayerID) error {
	tinyTxs := make([]types.TinyTx, 0, len(txs))
	for _, tx := range txs {
		tinyTxs = append(tinyTxs, types.AddressableTxToTiny(tx))
	}
	groupedTxs := groupByOrigin(tinyTxs)

	for addr, accountTxs := range groupedTxs {
		if err := m.addToAccountTxs(addr, accountTxs, layer); err != nil {
			return err
		}
	}
	return nil
}

func (m *MeshDB) addToAccountTxs(addr types.Address, accountTxs []types.TinyTx, layer types.LayerID) error {
	m.meshTxsMutex.Lock()
	defer m.meshTxsMutex.Unlock()

	// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
	pending, err := m.getAccountPendingTxs(addr)
	if err != nil {
		return err
	}
	pending.Add(accountTxs, layer)
	if err := m.storeAccountPendingTxs(addr, pending); err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) removeFromMeshTxs(accepted, rejected []*Transaction, layer types.LayerID) error {
	gAccepted := txsToTinyGrouped(accepted)
	gRejected := txsToTinyGrouped(rejected)
	accounts := make(map[types.Address]struct{})
	for account := range gAccepted {
		accounts[account] = struct{}{}
	}
	for account := range gRejected {
		accounts[account] = struct{}{}
	}

	for account := range accounts {
		if err := m.removeFromAccountTxs(account, gAccepted, gRejected, layer); err != nil {
			return err
		}
	}
	return nil
}

func (m *MeshDB) removeFromAccountTxs(account types.Address, gAccepted map[types.Address][]types.TinyTx, gRejected map[types.Address][]types.TinyTx, layer types.LayerID) error {
	m.meshTxsMutex.Lock()
	defer m.meshTxsMutex.Unlock()

	// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
	pending, err := m.getAccountPendingTxs(account)
	if err != nil {
		return err
	}
	pending.Remove(gAccepted[account], gRejected[account], layer)
	if err := m.storeAccountPendingTxs(account, pending); err != nil {
		return err
	}
	return nil
}

func txsToTinyGrouped(txs []*Transaction) map[types.Address][]types.TinyTx {
	tinyTxs := make([]types.TinyTx, 0, len(txs))
	for _, tx := range txs {
		tinyTxs = append(tinyTxs, txToTiny(tx))
	}
	return groupByOrigin(tinyTxs)
}

func (m *MeshDB) storeAccountPendingTxs(account types.Address, pending *pending_txs.AccountPendingTxs) error {
	if pending.IsEmpty() {
		if err := m.meshTxs.Delete(account.Bytes()); err != nil {
			return fmt.Errorf("failed to delete empty pending txs for account %v: %v", account.Short(), err)
		}
		return nil
	}
	if accountTxsBytes, err := types.InterfaceToBytes(&pending); err != nil {
		return fmt.Errorf("failed to marshal account pending txs: %v", err)
	} else if err := m.meshTxs.Put(account.Bytes(), accountTxsBytes); err != nil {
		return fmt.Errorf("failed to store mesh txs for address %s", account.Short())
	}
	return nil
}

func (m *MeshDB) getAccountPendingTxs(addr types.Address) (*pending_txs.AccountPendingTxs, error) {
	accountTxsBytes, err := m.meshTxs.Get(addr.Bytes())
	if err != nil && err != database.ErrNotFound {
		return nil, fmt.Errorf("failed to get mesh txs for account %s", addr.Short())
	}
	if err == database.ErrNotFound {
		return pending_txs.NewAccountPendingTxs(), nil
	}
	var pending pending_txs.AccountPendingTxs
	if err := types.BytesToInterface(accountTxsBytes, &pending); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account pending txs: %v", err)
	}
	return &pending, nil
}

func groupByOrigin(txs []types.TinyTx) map[types.Address][]types.TinyTx {
	grouped := make(map[types.Address][]types.TinyTx)
	for _, tx := range txs {
		grouped[tx.Origin] = append(grouped[tx.Origin], tx)
	}
	return grouped
}

type StateObj interface {
	Address() types.Address
	Nonce() uint64
	Balance() *big.Int
}

func (m *MeshDB) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	pending, err := m.getAccountPendingTxs(addr)
	if err != nil {
		return 0, 0, err
	}
	nonce, balance = pending.GetProjection(prevNonce, prevBalance)
	return nonce, balance, nil
}

func (m *MeshDB) GetTransactions(transactions []types.TransactionId) (
	map[types.TransactionId]*types.AddressableSignedTransaction,
	[]types.TransactionId) {

	var mIds []types.TransactionId
	ts := make(map[types.TransactionId]*types.AddressableSignedTransaction, len(transactions))
	for _, id := range transactions {
		t, err := m.GetTransaction(id)
		if err != nil {
			m.Warning("could not fetch tx, %v %v", hex.EncodeToString(id[:]), err)
			mIds = append(mIds, id)
		} else {
			ts[id] = t
		}
	}
	return ts, mIds
}

func (m *MeshDB) GetTransaction(id types.TransactionId) (*types.AddressableSignedTransaction, error) {
	tBytes, err := m.transactions.Get(id[:])
	if err != nil {
		return nil, fmt.Errorf("could not find transaction in database %v err=%v", hex.EncodeToString(id[:]), err)
	}
	return types.BytesAsAddressableTransaction(tBytes)
}

// ContextuallyValidBlock - returns the contextually valid blocks for the provided layer
func (m *MeshDB) ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error) {

	if layer == 0 || layer == 1 {
		v, err := m.LayerBlockIds(layer)
		if err != nil {
			m.Error("Could not get layer block ids for layer %v err=%v", layer, err)
			return nil, err
		}

		mp := make(map[types.BlockID]struct{}, len(v))
		for _, blk := range v {
			mp[blk] = struct{}{}
		}

		return mp, nil
	}

	blks, err := m.LayerBlocks(layer)
	if err != nil {
		return nil, err
	}

	validBlks := make(map[types.BlockID]struct{})

	for _, b := range blks {
		valid, err := m.ContextualValidity(b.ID())

		if err != nil {
			m.Error("could not get contextual validity for block %v in layer %v err=%v", b.ID(), layer, err)
		}

		if !valid {
			continue
		}

		validBlks[b.ID()] = struct{}{}
	}

	return validBlks, nil
}
