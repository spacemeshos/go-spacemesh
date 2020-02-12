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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type layerMutex struct {
	m            sync.Mutex
	layerWorkers uint32
}

type MeshDB struct {
	log.Log
	blockCache         blockCache
	layers             database.Database
	blocks             database.Database
	transactions       database.Database
	contextualValidity database.Database
	general            database.Database
	unappliedTxs       database.Database
	unappliedTxsMutex  sync.Mutex
	orphanBlocks       map[types.LayerID]map[types.BlockID]struct{}
	layerMutex         map[types.LayerID]*layerMutex
	lhMutex            sync.Mutex
}

func NewPersistentMeshDB(path string, blockCacheSize int, log log.Log) (*MeshDB, error) {
	bdb, err := database.NewLDBDatabase(filepath.Join(path, "blocks"), 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize blocks db: %v", err)
	}
	ldb, err := database.NewLDBDatabase(filepath.Join(path, "layers"), 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize layers db: %v", err)
	}
	vdb, err := database.NewLDBDatabase(filepath.Join(path, "validity"), 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize validity db: %v", err)
	}
	tdb, err := database.NewLDBDatabase(filepath.Join(path, "transactions"), 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize transactions db: %v", err)
	}
	gdb, err := database.NewLDBDatabase(filepath.Join(path, "general"), 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize general db: %v", err)
	}
	utx, err := database.NewLDBDatabase(filepath.Join(path, "unappliedTxs"), 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize mesh unappliedTxs db: %v", err)
	}

	ll := &MeshDB{
		Log:                log,
		blockCache:         NewBlockCache(blockCacheSize * layerSize),
		blocks:             bdb,
		layers:             ldb,
		transactions:       tdb,
		general:            gdb,
		contextualValidity: vdb,
		unappliedTxs:       utx,
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
	}
	return ll, nil
}

func (m *MeshDB) PersistentData() bool {
	if _, err := m.general.Get(LATEST); err == nil {
		m.Info("found data to recover on disc")
		return true
	}
	m.Info("did not find data to recover on disc")
	return false
}

func NewMemMeshDB(log log.Log) *MeshDB {
	ll := &MeshDB{
		Log:                log,
		blockCache:         NewBlockCache(100 * layerSize),
		blocks:             database.NewMemDatabase(),
		layers:             database.NewMemDatabase(),
		general:            database.NewMemDatabase(),
		contextualValidity: database.NewMemDatabase(),
		transactions:       database.NewMemDatabase(),
		unappliedTxs:       database.NewMemDatabase(),
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
	}
	return ll
}

func (m *MeshDB) Close() {
	m.blocks.Close()
	m.layers.Close()
	m.transactions.Close()
	m.unappliedTxs.Close()
	m.general.Close()
	m.contextualValidity.Close()
}

var ErrAlreadyExist = errors.New("block already exist in database")

func (m *MeshDB) AddBlock(bl *types.Block) error {
	if _, err := m.getBlockBytes(bl.Id()); err == nil {
		m.With().Warning(ErrAlreadyExist.Error(), log.BlockId(bl.Id().String()))
		return ErrAlreadyExist
	}
	if err := m.writeBlock(bl); err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) GetBlock(id types.BlockID) (*types.Block, error) {
	if id == GenesisBlock.Id() {
		//todo fit real genesis here
		return GenesisBlock, nil
	}

	if blkh := m.blockCache.Get(id); blkh != nil {
		return blkh, nil
	}

	b, err := m.getBlockBytes(id)
	if err != nil {
		return nil, err
	}
	mbk := &types.Block{}
	err = types.BytesToInterface(b, mbk)
	mbk.CalcAndSetId()
	return mbk, err
}

func (m *MeshDB) LayerBlocks(index types.LayerID) ([]*types.Block, error) {
	ids, err := m.LayerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blocks := make([]*types.Block, 0, len(ids))
	for _, k := range ids {
		block, err := m.GetBlock(k)
		if err != nil {
			return nil, fmt.Errorf("could not retrieve block %s %s", k.String(), err)
		}
		blocks = append(blocks, block)
	}

	return blocks, nil

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
			m.Log.With().Debug("ForBlockInView stopped", log.BlockId(block.Id().String()))
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

func (m *MeshDB) LayerBlockIds(index types.LayerID) ([]types.BlockID, error) {
	idsBytes, err := m.layers.Get(index.ToBytes())
	if err != nil {
		return nil, fmt.Errorf("error getting layer %v from database %v", index, err)
	}

	if len(idsBytes) == 0 {
		return nil, fmt.Errorf("no ids for layer %v in database ", index)
	}

	blockIds, err := types.BytesToBlockIds(idsBytes)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	return blockIds, nil
}

func (m *MeshDB) getBlockBytes(id types.BlockID) ([]byte, error) {
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
	m.Debug("save contextual validity %v %v", id, valid)
	return m.contextualValidity.Put(id.ToBytes(), v)
}

func (m *MeshDB) writeBlock(bl *types.Block) error {
	bytes, err := types.InterfaceToBytes(bl)
	if err != nil {
		return fmt.Errorf("could not encode bl")
	}

	if err := m.blocks.Put(bl.Id().ToBytes(), bytes); err != nil {
		return fmt.Errorf("could not add bl %v to database %v", bl.Id(), err)
	}

	m.updateLayerWithBlock(bl)

	m.blockCache.put(bl)

	return nil
}

func (m *MeshDB) updateLayerWithBlock(blk *types.Block) error {
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
	m.Debug("added block %v to layer %v", blk.Id(), blk.LayerIndex)
	blockIds = append(blockIds, blk.Id())
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

func getRewardKey(l types.LayerID, account types.Address) []byte {
	str := string(getRewardKeyPrefix(account)) + "_" + strconv.FormatUint(l.Uint64(), 10)
	return []byte(str)
}

func getRewardKeyPrefix(account types.Address) []byte {
	str := "reward_" + account.String()
	return []byte(str)
}

func getTransactionOriginKey(l types.LayerID, t *types.Transaction) []byte {
	str := string(getTransactionOriginKeyPrefix(l, t.Origin())) + "_" + t.Id().String()
	return []byte(str)
}

func getTransactionDestKey(l types.LayerID, t *types.Transaction) []byte {
	str := string(getTransactionDestKeyPrefix(l, t.Recipient)) + "_" + t.Id().String()
	return []byte(str)
}

func getTransactionOriginKeyPrefix(l types.LayerID, account types.Address) []byte {
	str := "a_o_" + account.String() + "_" + strconv.FormatUint(l.Uint64(), 10)
	return []byte(str)
}

func getTransactionDestKeyPrefix(l types.LayerID, account types.Address) []byte {
	str := "a_d_" + account.String() + "_" + strconv.FormatUint(l.Uint64(), 10)
	return []byte(str)
}

type DbTransaction struct {
	*types.Transaction
	Origin types.Address
}

func NewDbTransaction(tx *types.Transaction) *DbTransaction {
	return &DbTransaction{Transaction: tx, Origin: tx.Origin()}
}

func (t DbTransaction) GetTransaction() *types.Transaction {
	t.Transaction.SetOrigin(t.Origin)
	return t.Transaction
}

func (m *MeshDB) writeTransactions(l types.LayerID, txs []*types.Transaction) error {
	batch := m.transactions.NewBatch()
	for _, t := range txs {
		bytes, err := types.InterfaceToBytes(NewDbTransaction(t))
		if err != nil {
			return fmt.Errorf("could not marshall tx %v to bytes: %v", t.Id().ShortString(), err)
		}
		if err := batch.Put(t.Id().Bytes(), bytes); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.Id().ShortString(), err)
		}
		// write extra index for querying txs by account
		if err := batch.Put(getTransactionOriginKey(l, t), t.Id().Bytes()); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.Id().ShortString(), err)
		}
		if err := batch.Put(getTransactionDestKey(l, t), t.Id().Bytes()); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.Id().ShortString(), err)
		}
		m.Debug("wrote tx %v to db", t.Id().ShortString())
	}
	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transactions: %v", err)
	}
	return nil
}

type dbReward struct {
	TotalReward         uint64
	LayerRewardEstimate uint64
	// TotalReward - LayerRewardEstimate = FeesEstimate
}

func (m *MeshDB) writeTransactionRewards(l types.LayerID, accounts []types.Address, totalReward, layerReward *big.Int) error {
	actBlockCnt := make(map[types.Address]uint64)
	for _, account := range accounts {
		actBlockCnt[account]++
	}

	batch := m.transactions.NewBatch()
	for account, cnt := range actBlockCnt {
		reward := dbReward{TotalReward: cnt * totalReward.Uint64(), LayerRewardEstimate: cnt * layerReward.Uint64()}
		if b, err := types.InterfaceToBytes(&reward); err != nil {
			return fmt.Errorf("could not marshal reward for %v: %v", account.Short(), err)
		} else if err := batch.Put(getRewardKey(l, account), b); err != nil {
			return fmt.Errorf("could not write reward to %v to database: %v", account.Short(), err)
		}
	}
	return batch.Write()
}

func (m *MeshDB) GetRewards(account types.Address) (rewards []types.Reward, err error) {
	it := m.transactions.Find(getRewardKeyPrefix(account))
	for it.Next() {
		if it.Key() == nil {
			break
		}
		str := string(it.Key())
		strs := strings.Split(str, "_")
		layer, err := strconv.ParseUint(strs[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("wrong key in db %s: %v", it.Key(), err)
		}
		var reward dbReward
		err = types.BytesToInterface(it.Value(), &reward)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal reward: %v", err)
		}
		rewards = append(rewards, types.Reward{
			Layer:               types.LayerID(layer),
			TotalReward:         reward.TotalReward,
			LayerRewardEstimate: reward.LayerRewardEstimate,
		})
	}
	return
}

func (m *MeshDB) addToUnappliedTxs(txs []*types.Transaction, layer types.LayerID) error {
	groupedTxs := groupByOrigin(txs)

	for addr, accountTxs := range groupedTxs {
		if err := m.addToAccountTxs(addr, accountTxs, layer); err != nil {
			return err
		}
	}
	return nil
}

func (m *MeshDB) addToAccountTxs(addr types.Address, accountTxs []*types.Transaction, layer types.LayerID) error {
	m.unappliedTxsMutex.Lock()
	defer m.unappliedTxsMutex.Unlock()

	// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
	pending, err := m.getAccountPendingTxs(addr)
	if err != nil {
		return err
	}
	pending.Add(layer, accountTxs...)
	if err := m.storeAccountPendingTxs(addr, pending); err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) removeFromUnappliedTxs(accepted, rejected []*types.Transaction, layer types.LayerID) {
	gAccepted := groupByOrigin(accepted)
	gRejected := groupByOrigin(rejected)
	accounts := make(map[types.Address]struct{})
	for account := range gAccepted {
		accounts[account] = struct{}{}
	}
	for account := range gRejected {
		accounts[account] = struct{}{}
	}

	for account := range accounts {
		m.removeFromAccountTxs(account, gAccepted, gRejected, layer)
	}
}

func (m *MeshDB) removeFromAccountTxs(account types.Address, gAccepted, gRejected map[types.Address][]*types.Transaction, layer types.LayerID) {
	m.unappliedTxsMutex.Lock()
	defer m.unappliedTxsMutex.Unlock()

	// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
	pending, err := m.getAccountPendingTxs(account)
	if err != nil {
		m.With().Error("failed to get account pending txs",
			log.LayerId(uint64(layer)), log.String("address", account.Short()), log.Err(err))
		return
	}
	pending.Remove(gAccepted[account], gRejected[account], layer)
	if err := m.storeAccountPendingTxs(account, pending); err != nil {
		m.With().Error("failed to store account pending txs",
			log.LayerId(uint64(layer)), log.String("address", account.Short()), log.Err(err))
	}
}

func (m *MeshDB) storeAccountPendingTxs(account types.Address, pending *pending_txs.AccountPendingTxs) error {
	if pending.IsEmpty() {
		if err := m.unappliedTxs.Delete(account.Bytes()); err != nil {
			return fmt.Errorf("failed to delete empty pending txs for account %v: %v", account.Short(), err)
		}
		return nil
	}
	if accountTxsBytes, err := types.InterfaceToBytes(&pending); err != nil {
		return fmt.Errorf("failed to marshal account pending txs: %v", err)
	} else if err := m.unappliedTxs.Put(account.Bytes(), accountTxsBytes); err != nil {
		return fmt.Errorf("failed to store mesh txs for address %s", account.Short())
	}
	return nil
}

func (m *MeshDB) getAccountPendingTxs(addr types.Address) (*pending_txs.AccountPendingTxs, error) {
	accountTxsBytes, err := m.unappliedTxs.Get(addr.Bytes())
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

func groupByOrigin(txs []*types.Transaction) map[types.Address][]*types.Transaction {
	grouped := make(map[types.Address][]*types.Transaction)
	for _, tx := range txs {
		grouped[tx.Origin()] = append(grouped[tx.Origin()], tx)
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

type txGetter struct {
	missingIds map[types.TransactionId]struct{}
	txs        []*types.Transaction
	mesh       *MeshDB
}

func (g *txGetter) Get(id types.TransactionId) {
	t, err := g.mesh.GetTransaction(id)
	if err != nil {
		g.mesh.With().Warning("could not fetch tx", log.TxId(id.ShortString()), log.Err(err))
		g.missingIds[id] = struct{}{}
	} else {
		g.txs = append(g.txs, t)
	}
}

func newGetter(m *MeshDB) *txGetter {
	return &txGetter{mesh: m, missingIds: make(map[types.TransactionId]struct{})}
}

func (m *MeshDB) GetTransactions(transactions []types.TransactionId) ([]*types.Transaction, map[types.TransactionId]struct{}) {
	getter := newGetter(m)
	for _, id := range transactions {
		getter.Get(id)
	}
	return getter.txs, getter.missingIds
}

func (m *MeshDB) GetTransaction(id types.TransactionId) (*types.Transaction, error) {
	tBytes, err := m.transactions.Get(id[:])
	if err != nil {
		return nil, fmt.Errorf("could not find transaction in database %v err=%v", hex.EncodeToString(id[:]), err)
	}
	var dbTx DbTransaction
	err = types.BytesToInterface(tBytes, &dbTx)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %v", err)
	}
	return dbTx.GetTransaction(), nil
}

func (m *MeshDB) GetTransactionsByDestination(l types.LayerID, account types.Address) (txs []types.TransactionId) {
	it := m.transactions.Find(getTransactionDestKeyPrefix(l, account))
	for it.Next() {
		if it.Key() == nil {
			break
		}
		var a types.TransactionId
		err := types.BytesToInterface(it.Value(), &a)
		if err != nil {
			//log error
			break
		}
		txs = append(txs, a)
	}
	return
}

func (m *MeshDB) GetTransactionsByOrigin(l types.LayerID, account types.Address) (txs []types.TransactionId) {
	it := m.transactions.Find(getTransactionOriginKeyPrefix(l, account))
	for it.Next() {
		if it.Key() == nil {
			break
		}
		var a types.TransactionId
		err := types.BytesToInterface(it.Value(), &a)
		if err != nil {
			//log error
			break
		}
		txs = append(txs, a)
	}
	return
}

func (m *MeshDB) BlocksByValidity(blocks []*types.Block) (validBlocks, invalidBlocks []*types.Block) {
	for _, b := range blocks {
		valid, err := m.ContextualValidity(b.Id())
		if err != nil {
			m.With().Error("could not get contextual validity", log.BlockId(b.Id().String()), log.Err(err))
		}
		if valid {
			validBlocks = append(validBlocks, b)
		} else {
			invalidBlocks = append(invalidBlocks, b)
		}
	}
	return validBlocks, invalidBlocks
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
		valid, err := m.ContextualValidity(b.Id())

		if err != nil {
			m.Error("could not get contextual validity for block %v in layer %v err=%v", b.Id(), layer, err)
		}

		if !valid {
			continue
		}

		validBlks[b.Id()] = struct{}{}
	}

	return validBlks, nil
}

func (m *MeshDB) Persist(key []byte, v interface{}) error {
	buf, err := types.InterfaceToBytes(v)
	if err != nil {
		panic(err)
	}
	return m.general.Put(key, buf)
}

func (m *MeshDB) Retrieve(key []byte, v interface{}) (interface{}, error) {
	val, err := m.general.Get(key)
	if err != nil {
		m.Warning("failed retrieving object from db ", err)
		return nil, err
	}

	if val == nil {
		return nil, fmt.Errorf("no such value in database db ")
	}

	if err := types.BytesToInterface(val, v); err != nil {
		return nil, fmt.Errorf("failed decoding object from db %v", err)
	}

	return v, nil
}

func (m *MeshDB) CacheWarmUp(from types.LayerID, to types.LayerID) error {
	for i := from; i < to; i++ {

		layer, err := m.LayerBlockIds(i)
		if err != nil {
			return fmt.Errorf("could not get layer %v from database %v", layer, err)
		}

		for _, b := range layer {
			block, blockErr := m.GetBlock(b)
			if blockErr != nil {
				return fmt.Errorf("could not get bl %v from database %v", b, blockErr)
			}
			m.blockCache.put(block)
		}

	}

	return nil
}
