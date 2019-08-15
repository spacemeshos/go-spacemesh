package mesh

import (
	"container/list"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
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
	meshTxs            database.Database
	orphanBlocks       map[types.LayerID]map[types.BlockID]struct{}
	layerMutex         map[types.LayerID]*layerMutex
	lhMutex            sync.Mutex
}

func NewPersistentMeshDB(path string, log log.Log) (*MeshDB, error) {
	bdb := database.NewLevelDbStore(path+"blocks", nil, nil)
	ldb := database.NewLevelDbStore(path+"layers", nil, nil)
	vdb := database.NewLevelDbStore(path+"validity", nil, nil)
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

func (m *MeshDB) AddBlock(bl *types.Block) error {
	if _, err := m.getMiniBlockBytes(bl.ID()); err == nil {
		return errors.New(fmt.Sprintf("block %v already exists in database", bl.ID()))
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

func (m *MeshDB) ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) error) error {
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
		if err := blockHandler(block); err != nil {
			return err
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

func (m *MeshDB) getContextualValidity(id types.BlockID) (bool, error) {
	b, err := m.contextualValidity.Get(id.ToBytes())
	if err != nil {
		return false, nil
	}
	return b[0] == 1, err //bytes to bool
}

func (m *MeshDB) setContextualValidity(id types.BlockID, valid bool) error {
	//todo implement
	//todo concurrency
	var v []byte
	if valid {
		v = TRUE
	} else {
		v = FALSE
	}
	m.contextualValidity.Put(id.ToBytes(), v)
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

type tinyTx struct {
	Id     types.TransactionId
	Origin address.Address
	Nonce  uint64
	Amount uint64
	//Fee    uint64
}

func addressableTxToTiny(tx *types.AddressableSignedTransaction) tinyTx {
	// TODO: calculate and store fee amount
	id := types.GetTransactionId(tx.SerializableSignedTransaction)
	return tinyTx{
		Id:     id,
		Origin: tx.Address,
		Nonce:  tx.AccountNonce,
		Amount: tx.Amount,
	}
}

func txToTiny(tx *Transaction) tinyTx {
	// TODO: calculate and store fee amount
	id := types.GetTransactionId(Transaction2SerializableTransaction(tx).SerializableSignedTransaction)
	return tinyTx{
		Id:     id,
		Origin: tx.Origin,
		Nonce:  tx.AccountNonce,
		Amount: tx.Amount.Uint64(),
	}
}

func (m *MeshDB) addToMeshTxs(txs []*types.AddressableSignedTransaction, layer types.LayerID) error {
	// TODO: lock
	tinyTxs := make([]tinyTx, 0, len(txs))
	for _, tx := range txs {
		tinyTxs = append(tinyTxs, addressableTxToTiny(tx))
	}
	groupedTxs := groupByOrigin(tinyTxs)

	for account, accountTxs := range groupedTxs {
		// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
		pending, err := m.getAccountPendingTxs(account)
		if err != nil {
			return err
		}
		pending.Add(accountTxs, layer)
		if err := m.storeAccountPendingTxs(account, pending); err != nil {
			return err
		}
	}
	return nil
}

func (m *MeshDB) removeFromMeshTxs(accepted, rejected []*Transaction, layer types.LayerID) error {
	// TODO: lock
	gAccepted := txsToTinyGrouped(accepted)
	gRejected := txsToTinyGrouped(rejected)
	accounts := make(map[address.Address]struct{})
	for account := range gAccepted {
		accounts[account] = struct{}{}
	}
	for account := range gRejected {
		accounts[account] = struct{}{}
	}

	for account := range accounts {
		// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
		pending, err := m.getAccountPendingTxs(account)
		if err != nil {
			return err
		}
		pending.Remove(gAccepted[account], gRejected[account], layer)
		if err := m.storeAccountPendingTxs(account, pending); err != nil {
			return err
		}
	}
	return nil
}

func txsToTinyGrouped(txs []*Transaction) map[address.Address][]tinyTx {
	tinyTxs := make([]tinyTx, 0, len(txs))
	for _, tx := range txs {
		tinyTxs = append(tinyTxs, txToTiny(tx))
	}
	return groupByOrigin(tinyTxs)
}

func (m *MeshDB) storeAccountPendingTxs(account address.Address, pending *accountPendingTxs) error {
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

func (m *MeshDB) getAccountPendingTxs(account address.Address) (*accountPendingTxs, error) {
	accountTxsBytes, err := m.meshTxs.Get(account.Bytes())
	if err != nil && err != database.ErrNotFound {
		return nil, fmt.Errorf("failed to get mesh txs for account %s", account.Short())
	}
	if err == database.ErrNotFound {
		return newAccountPendingTxs(), nil
	}
	var pending accountPendingTxs
	if err := types.BytesToInterface(accountTxsBytes, &pending); err != nil {
		return nil, fmt.Errorf("failed to unmarshal account pending txs: %v", err)
	}
	return &pending, nil
}

func groupByOrigin(txs []tinyTx) map[address.Address][]tinyTx {
	grouped := make(map[address.Address][]tinyTx)
	for _, tx := range txs {
		grouped[tx.Origin] = append(grouped[tx.Origin], tx)
	}
	return grouped
}

type StateObj interface {
	Address() address.Address
	Nonce() uint64
	Balance() *big.Int
}

func (m *MeshDB) GetStateProjection(stateObj StateObj) (nonce uint64, balance uint64, err error) {
	pending, err := m.getAccountPendingTxs(stateObj.Address())
	if err != nil {
		return 0, 0, err
	}
	nonce, balance = pending.GetProjection(stateObj.Nonce(), stateObj.Balance().Uint64())
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
