package mesh

import (
	"container/list"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
)

type layerMutex struct {
	m            sync.Mutex
	layerWorkers uint32
}

// DB represents a mesh database instance
type DB struct {
	log.Log
	blockCache            blockCache
	layers                database.Database
	blocks                database.Database
	transactions          database.Database
	contextualValidity    database.Database
	general               database.Database
	unappliedTxs          database.Database
	inputVector           database.Database
	unappliedTxsMutex     sync.Mutex
	blockMutex            sync.RWMutex
	orphanBlocks          map[types.LayerID]map[types.BlockID]struct{}
	layerMutex            map[types.LayerID]*layerMutex
	lhMutex               sync.Mutex
	InputVectorBackupFunc func(id types.LayerID) ([]types.BlockID, error)
	exit                  chan struct{}
}

// NewPersistentMeshDB creates an instance of a mesh database
func NewPersistentMeshDB(path string, blockCacheSize int, log log.Log) (*DB, error) {
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
	iv, err := database.NewLDBDatabase(filepath.Join(path, "inputvector"), 0, 0, log)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize mesh unappliedTxs db: %v", err)
	}

	ll := &DB{
		Log:                log,
		blockCache:         newBlockCache(blockCacheSize * layerSize),
		blocks:             bdb,
		layers:             ldb,
		transactions:       tdb,
		general:            gdb,
		contextualValidity: vdb,
		unappliedTxs:       utx,
		inputVector:        iv,
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
		exit:               make(chan struct{}),
	}

	for _, blk := range GenesisLayer().Blocks() {
		ll.Log.With().Info("Adding genesis block ", blk.ID(), blk.LayerIndex)
		if err := ll.AddBlock(blk); err != nil {
			log.With().Error("Error inserting genesis block to db", blk.ID(), blk.LayerIndex)
		}
		if err := ll.SaveContextualValidity(blk.ID(), true); err != nil {
			log.With().Error("Error inserting genesis block to db", blk.ID(), blk.LayerIndex)
		}
	}
	if err := ll.SaveLayerInputVectorByID(GenesisLayer().Index(), types.BlockIDs(GenesisLayer().Blocks())); err != nil {
		log.With().Error("Error inserting genesis input vector to db", GenesisLayer().Index())
	}
	return ll, err
}

// PersistentData checks to see if db is empty
func (m *DB) PersistentData() bool {
	if _, err := m.general.Get(constLATEST); err == nil {
		m.Info("found data to recover on disk")
		return true
	}
	m.Info("did not find data to recover on disk")
	return false
}

// NewMemMeshDB is a mock used for testing
func NewMemMeshDB(log log.Log) *DB {
	ll := &DB{
		Log:                log,
		blockCache:         newBlockCache(100 * layerSize),
		blocks:             database.NewMemDatabase(),
		layers:             database.NewMemDatabase(),
		general:            database.NewMemDatabase(),
		contextualValidity: database.NewMemDatabase(),
		transactions:       database.NewMemDatabase(),
		unappliedTxs:       database.NewMemDatabase(),
		inputVector:        database.NewMemDatabase(),
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
		exit:               make(chan struct{}),
	}
	for _, blk := range GenesisLayer().Blocks() {
		ll.AddBlock(blk)
		ll.SaveContextualValidity(blk.ID(), true)
	}
	if err := ll.SaveLayerInputVectorByID(GenesisLayer().Index(), types.BlockIDs(GenesisLayer().Blocks())); err != nil {
		log.With().Error("Error inserting genesis input vector to db", GenesisLayer().Index())
	}
	return ll
}

// Close closes all resources
func (m *DB) Close() {
	close(m.exit)
	m.blocks.Close()
	m.layers.Close()
	m.transactions.Close()
	m.unappliedTxs.Close()
	m.inputVector.Close()
	m.general.Close()
	m.contextualValidity.Close()
}

//todo: for now, these methods are used to export dbs to sync, think about merging the two packages

// Blocks exports the block database
func (m *DB) Blocks() database.Database {
	m.blockMutex.RLock()
	defer m.blockMutex.RUnlock()
	return m.blocks
}

// Transactions exports the transactions DB
func (m *DB) Transactions() database.Database {
	return m.transactions
}

// InputVector exports the inputvector DB
func (m *DB) InputVector() database.Database {
	return m.inputVector
}

// ErrAlreadyExist error returned when adding an existing value to the database
var ErrAlreadyExist = errors.New("block already exists in database")

// AddBlock adds a block to the database
func (m *DB) AddBlock(bl *types.Block) error {
	m.blockMutex.Lock()
	defer m.blockMutex.Unlock()
	if _, err := m.getBlockBytes(bl.ID()); err == nil {
		m.With().Warning(ErrAlreadyExist.Error(), bl.ID())
		return ErrAlreadyExist
	}
	if err := m.writeBlock(bl); err != nil {
		return err
	}
	return nil
}

// GetBlock gets a block from the database by id
func (m *DB) GetBlock(id types.BlockID) (*types.Block, error) {
	if blkh := m.blockCache.Get(id); blkh != nil {
		return blkh, nil
	}

	b, err := m.getBlockBytes(id)
	if err != nil {
		return nil, err
	}
	mbk := &types.Block{}
	err = types.BytesToInterface(b, mbk)
	mbk.Initialize()
	return mbk, err
}

// LayerBlocks retrieves all blocks from a layer by layer index
func (m *DB) LayerBlocks(index types.LayerID) ([]*types.Block, error) {
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

// ForBlockInView traverses all blocks in a view and uses blockHandler func on each block
// The block handler func should return two values - a bool indicating whether or not we should stop traversing after the current block (happy flow)
// and an error indicating that an error occurred while handling the block, the traversing will stop in that case as well (error flow)
func (m *DB) ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) (bool, error)) error {
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

		// catch blocks that were referenced after more than one layer, and slipped through the stop condition
		if block.LayerIndex < layer {
			continue
		}

		// execute handler
		stop, err := blockHandler(block)
		if err != nil {
			return err
		}

		if stop {
			m.Log.With().Debug("ForBlockInView stopped", block.ID())
			break
		}

		// stop condition: referenced blocks must be in lower layers, so we don't traverse them
		if block.LayerIndex == layer {
			continue
		}

		// push children to bfs queue
		for _, id := range append(block.ForDiff, append(block.AgainstDiff, block.NeutralDiff...)...) {
			if _, found := seenBlocks[id]; !found {
				seenBlocks[id] = struct{}{}
				blocksToVisit.PushBack(id)
			}
		}
	}
	return nil
}

// LayerBlockIds retrieves all block ids from a layer by layer index
func (m *DB) LayerBlockIds(index types.LayerID) ([]types.BlockID, error) {
	idsBytes, err := m.layers.Get(index.Bytes())
	if err != nil {
		return nil, err
	}

	if len(idsBytes) == 0 {
		//zero block layer
		return []types.BlockID{}, nil
	}

	blockIds, err := types.BytesToBlockIds(idsBytes)
	if err != nil {
		return nil, errors.New("could not get all blocks from database")
	}

	return blockIds, nil
}

// AddZeroBlockLayer tags lyr as a layer without blocks
func (m *DB) AddZeroBlockLayer(index types.LayerID) error {
	blockIds := make([]types.BlockID, 0, 1)
	w, err := types.BlockIdsToBytes(blockIds)
	if err != nil {
		return errors.New("could not encode layer blk ids")
	}
	return m.layers.Put(index.Bytes(), w)
}

func (m *DB) getBlockBytes(id types.BlockID) ([]byte, error) {
	return m.blocks.Get(id.AsHash32().Bytes())
}

// ContextualValidity retrieves opinion on block from the database
func (m *DB) ContextualValidity(id types.BlockID) (bool, error) {
	b, err := m.contextualValidity.Get(id.Bytes())
	if err != nil {
		return false, err
	}
	return b[0] == 1, nil // bytes to bool
}

// SaveContextualValidity persists opinion on block to the database
func (m *DB) SaveContextualValidity(id types.BlockID, valid bool) error {
	var v []byte
	if valid {
		v = constTrue
	} else {
		v = constFalse
	}
	m.Debug("save contextual validity %v %v", id, valid)
	return m.contextualValidity.Put(id.Bytes(), v)
}

// SaveLayerInputVector saves the input vote vector for a layer (hare results)
func (m *DB) SaveLayerInputVector(hash types.Hash32, vector []types.BlockID) error {
	bytes, err := types.InterfaceToBytes(vector)
	if err != nil {
		return err
	}

	return m.inputVector.Put(hash.Bytes(), bytes)
}

func (m *DB) defaulGetLayerInputVectorByID(id types.LayerID) ([]types.BlockID, error) {
	by, err := m.inputVector.Get(types.CalcHash32(id.Bytes()).Bytes())
	if err != nil {
		return nil, err
	}
	var v []types.BlockID
	err = types.BytesToInterface(by, &v)
	return v, err
}

// GetLayerInputVector gets the input vote vector for a layer (hare results)
func (m *DB) GetLayerInputVector(hash types.Hash32) ([]types.BlockID, error) {
	by, err := m.inputVector.Get(hash.Bytes())
	if err != nil {
		return nil, err
	}
	var v []types.BlockID
	err = types.BytesToInterface(by, &v)
	return v, err
}

// GetLayerInputVectorByID gets the input vote vector for a layer (hare results)
func (m *DB) GetLayerInputVectorByID(id types.LayerID) ([]types.BlockID, error) {
	if m.InputVectorBackupFunc != nil {
		return m.InputVectorBackupFunc(id)
	}
	return m.defaulGetLayerInputVectorByID(id)
}

// SaveLayerInputVectorByID gets the input vote vector for a layer (hare results)
func (m *DB) SaveLayerInputVectorByID(id types.LayerID, blks []types.BlockID) error {
	hash := types.CalcHash32(id.Bytes())
	m.With().Info("SaveLayerInputVectorByID: Saving input vector", id, hash)
	return m.SaveLayerInputVector(hash, blks)
}

// SaveLayerHashInputVector saves the input vote vector for a layer (hare results) using its hash
func (m *DB) SaveLayerHashInputVector(h types.Hash32, data []byte) error {
	m.Info("saved input vector for hash %v", h.ShortString())
	return m.inputVector.Put(h.Bytes(), data)
}

func (m *DB) writeBlock(bl *types.Block) error {
	bytes, err := types.InterfaceToBytes(bl)
	if err != nil {
		return fmt.Errorf("could not encode bl")
	}

	if err := m.blocks.Put(bl.ID().AsHash32().Bytes(), bytes); err != nil {
		return fmt.Errorf("could not add bl %v to database %v", bl.ID(), err)
	}

	m.updateLayerWithBlock(bl)

	m.blockCache.put(bl)

	return nil
}

func (m *DB) updateLayerWithBlock(blk *types.Block) error {
	lm := m.getLayerMutex(blk.LayerIndex)
	defer m.endLayerWorker(blk.LayerIndex)
	lm.m.Lock()
	defer lm.m.Unlock()
	ids, err := m.layers.Get(blk.LayerIndex.Bytes())
	var blockIds []types.BlockID
	if err != nil {
		// layer doesnt exist, need to insert new layer
		blockIds = make([]types.BlockID, 0, 1)
	} else {
		blockIds, err = types.BytesToBlockIds(ids)
		if err != nil {
			return errors.New("could not get all blocks from database ")
		}
	}
	m.Debug("added block %v to layer %v", blk.ID(), blk.LayerIndex)
	blockIds = append(blockIds, blk.ID())
	types.SortBlockIDs(blockIds)
	w, err := types.BlockIdsToBytes(blockIds)
	if err != nil {
		return errors.New("could not encode layer blk ids")
	}
	m.layers.Put(blk.LayerIndex.Bytes(), w)
	hash := types.CalcBlocksHash32(blockIds, nil)
	m.persistLayerHash(blk.LayerIndex, hash)

	return nil
}

func (m *DB) getLayerHashKey(layerID types.LayerID) []byte {
	return []byte(fmt.Sprintf("layerHash_%v", layerID.Bytes()))
}

// GetLayerHash returns layer hash for received blocks
func (m *Mesh) GetLayerHash(layerID types.LayerID) types.Hash32 {
	h := types.Hash32{}
	bts, err := m.general.Get(m.getLayerHashKey(layerID))
	if err != nil {
		return types.Hash32{}
	}
	h.SetBytes(bts)
	return h
}

func (m *DB) persistLayerHash(layerID types.LayerID, hash types.Hash32) {
	if err := m.general.Put(m.getLayerHashKey(layerID), hash.Bytes()); err != nil {
		m.With().Error("failed to persist layer hash", log.Err(err), layerID,
			log.String("layer_hash", hash.Hex()))
	}

	// we store a double index here because most of the code uses layer ID as key, currently only sync reads layer by hash
	// when this changes we can simply point to the layes
	if err := m.general.Put(hash.Bytes(), layerID.Bytes()); err != nil {
		m.With().Error("failed to persist layer hash", log.Err(err), layerID,
			log.String("layer_hash", hash.Hex()))
	}
}

// try delete layer Handler (deletes if pending pendingCount is 0)
func (m *DB) endLayerWorker(index types.LayerID) {
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

// returns the existing layer Handler (crates one if doesn't exist)
func (m *DB) getLayerMutex(index types.LayerID) *layerMutex {
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

// Schema: "r_<coinbase>_<smesherId>_<layerId> -> reward struct"
func getRewardKey(l types.LayerID, account types.Address, smesherID types.NodeID) []byte {
	str := string(getRewardKeyPrefix(account)) + "_" + smesherID.String() + "_" + strconv.FormatUint(l.Uint64(), 10)
	return []byte(str)
}

func getRewardKeyPrefix(account types.Address) []byte {
	str := "r_" + account.String()
	return []byte(str)
}

// This function gets the reward key for a particular smesherID
// format for the index "s_<smesherid>_<accountid>_<layerid> -> r_<accountid>_<smesherid>_<layerid> -> the actual reward"
func getSmesherRewardKey(l types.LayerID, smesherID types.NodeID, account types.Address) []byte {
	str := string(getSmesherRewardKeyPrefix(smesherID)) + "_" + account.String() + "_" + strconv.FormatUint(l.Uint64(), 10)
	return []byte(str)
}

//use r_ for one and s_ for the other so the namespaces can't collide
func getSmesherRewardKeyPrefix(smesherID types.NodeID) []byte {
	str := "s_" + smesherID.String()
	return []byte(str)
}

func getTransactionOriginKey(l types.LayerID, t *types.Transaction) []byte {
	str := string(getTransactionOriginKeyPrefix(l, t.Origin())) + "_" + t.ID().String()
	return []byte(str)
}

func getTransactionDestKey(l types.LayerID, t *types.Transaction) []byte {
	str := string(getTransactionDestKeyPrefix(l, t.Recipient)) + "_" + t.ID().String()
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

// DbTransaction is the transaction type stored in DB
type DbTransaction struct {
	*types.Transaction
	Origin types.Address
}

func newDbTransaction(tx *types.Transaction) *DbTransaction {
	return &DbTransaction{Transaction: tx, Origin: tx.Origin()}
}

func (t DbTransaction) getTransaction() *types.Transaction {
	t.Transaction.SetOrigin(t.Origin)
	return t.Transaction
}

func (m *DB) writeTransactions(l types.LayerID, txs []*types.Transaction) error {
	batch := m.transactions.NewBatch()
	for _, t := range txs {
		bytes, err := types.InterfaceToBytes(newDbTransaction(t))
		if err != nil {
			return fmt.Errorf("could not marshall tx %v to bytes: %v", t.ID().ShortString(), err)
		}
		m.Log.Info("storing tx id %v size %v", t.ID().ShortString(), len(bytes))
		if err := batch.Put(t.ID().Bytes(), bytes); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
		}
		// write extra index for querying txs by account
		if err := batch.Put(getTransactionOriginKey(l, t), t.ID().Bytes()); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
		}
		if err := batch.Put(getTransactionDestKey(l, t), t.ID().Bytes()); err != nil {
			return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
		}
		m.Debug("wrote tx %v to db", t.ID().ShortString())
	}
	err := batch.Write()
	if err != nil {
		return fmt.Errorf("failed to write transactions: %v", err)
	}
	return nil
}

// WriteTransaction writes a single transaction to the db
func (m *DB) WriteTransaction(l types.LayerID, t *types.Transaction) error {
	bytes, err := types.InterfaceToBytes(newDbTransaction(t))
	if err != nil {
		return fmt.Errorf("could not marshall tx %v to bytes: %v", t.ID().ShortString(), err)
	}
	if err := m.transactions.Put(t.ID().Bytes(), bytes); err != nil {
		return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
	}
	// write extra index for querying txs by account
	if err := m.transactions.Put(getTransactionOriginKey(l, t), t.ID().Bytes()); err != nil {
		return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
	}
	if err := m.transactions.Put(getTransactionDestKey(l, t), t.ID().Bytes()); err != nil {
		return fmt.Errorf("could not write tx %v to database: %v", t.ID().ShortString(), err)
	}
	m.Debug("wrote tx %v to db", t.ID().ShortString())

	return nil
}

//We're not using the existing reward type because the layer is implicit in the key
type dbReward struct {
	TotalReward         uint64
	LayerRewardEstimate uint64
	SmesherID           types.NodeID
	Coinbase            types.Address
	// TotalReward - LayerRewardEstimate = FeesEstimate
}

func (m *DB) writeTransactionRewards(l types.LayerID, accountBlockCount map[types.Address]map[string]uint64, totalReward, layerReward *big.Int) error {
	batch := m.transactions.NewBatch()
	for account, smesherAccountEntry := range accountBlockCount {
		for smesherString, cnt := range smesherAccountEntry {
			smesherEntry, err := types.StringToNodeID(smesherString)
			if err != nil {
				return fmt.Errorf("could not convert String to NodeID for %v: %v", smesherString, err)
			}
			reward := dbReward{TotalReward: cnt * totalReward.Uint64(), LayerRewardEstimate: cnt * layerReward.Uint64(), SmesherID: *smesherEntry, Coinbase: account}
			if b, err := types.InterfaceToBytes(&reward); err != nil {
				return fmt.Errorf("could not marshal reward for %v: %v", account.Short(), err)
			} else if err := batch.Put(getRewardKey(l, account, *smesherEntry), b); err != nil {
				return fmt.Errorf("could not write reward to %v to database: %v", account.Short(), err)
			} else if err := batch.Put(getSmesherRewardKey(l, *smesherEntry, account), getRewardKey(l, account, *smesherEntry)); err != nil {
				return fmt.Errorf("could not write reward key for smesherID %v to database: %v", smesherEntry.ShortString(), err)
			}
		}
	}
	return batch.Write()
}

// GetRewards retrieves account's rewards by address
func (m *DB) GetRewards(account types.Address) (rewards []types.Reward, err error) {
	it := m.transactions.Find(getRewardKeyPrefix(account))
	for it.Next() {
		if it.Key() == nil {
			break
		}
		str := string(it.Key())
		strs := strings.Split(str, "_")
		layer, err := strconv.ParseUint(strs[3], 10, 64)
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
			SmesherID:           reward.SmesherID,
			Coinbase:            reward.Coinbase,
		})
	}
	return
}

// GetRewardsBySmesherID retrieves rewards by smesherID
func (m *DB) GetRewardsBySmesherID(smesherID types.NodeID) (rewards []types.Reward, err error) {
	it := m.transactions.Find(getSmesherRewardKeyPrefix(smesherID))
	for it.Next() {
		if it.Key() == nil {
			break
		}
		str := string(it.Key())
		strs := strings.Split(str, "_")
		layer, err := strconv.ParseUint(strs[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("error parsing db key %s: %v", it.Key(), err)
		}
		//find the key to the actual reward struct, which is in it.Value()
		var reward dbReward
		rewardBytes, err := m.transactions.Get(it.Value())

		if err != nil {
			return nil, fmt.Errorf("wrong key in db %s: %v", it.Value(), err)
		}
		if err = types.BytesToInterface(rewardBytes, &reward); err != nil {
			return nil, fmt.Errorf("failed to unmarshal reward: %v", err)
		}
		rewards = append(rewards, types.Reward{
			Layer:               types.LayerID(layer),
			TotalReward:         reward.TotalReward,
			LayerRewardEstimate: reward.LayerRewardEstimate,
			SmesherID:           reward.SmesherID,
			Coinbase:            reward.Coinbase,
		})
	}
	return
}

func (m *DB) addToUnappliedTxs(txs []*types.Transaction, layer types.LayerID) error {
	groupedTxs := groupByOrigin(txs)

	for addr, accountTxs := range groupedTxs {
		if err := m.addToAccountTxs(addr, accountTxs, layer); err != nil {
			return err
		}
	}
	return nil
}

func (m *DB) addToAccountTxs(addr types.Address, accountTxs []*types.Transaction, layer types.LayerID) error {
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

func (m *DB) removeFromUnappliedTxs(accepted []*types.Transaction) (grouped map[types.Address][]*types.Transaction, accounts map[types.Address]struct{}) {
	grouped = groupByOrigin(accepted)
	accounts = make(map[types.Address]struct{})
	for account := range grouped {
		accounts[account] = struct{}{}
	}
	for account := range accounts {
		m.removeFromAccountTxs(account, grouped)
	}
	return
}

func (m *DB) removeFromAccountTxs(account types.Address, gAccepted map[types.Address][]*types.Transaction) {
	m.unappliedTxsMutex.Lock()
	defer m.unappliedTxsMutex.Unlock()

	// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
	pending, err := m.getAccountPendingTxs(account)
	if err != nil {
		m.With().Error("failed to get account pending txs",
			log.String("address", account.Short()), log.Err(err))
		return
	}
	pending.RemoveAccepted(gAccepted[account])
	if err := m.storeAccountPendingTxs(account, pending); err != nil {
		m.With().Error("failed to store account pending txs",
			log.String("address", account.Short()), log.Err(err))
	}
}

func (m *DB) removeRejectedFromAccountTxs(account types.Address, rejected map[types.Address][]*types.Transaction, layer types.LayerID) {
	m.unappliedTxsMutex.Lock()
	defer m.unappliedTxsMutex.Unlock()

	// TODO: instead of storing a list, use LevelDB's prefixed keys and then iterate all relevant keys
	pending, err := m.getAccountPendingTxs(account)
	if err != nil {
		m.With().Error("failed to get account pending txs",
			layer, log.String("address", account.Short()), log.Err(err))
		return
	}
	pending.RemoveRejected(rejected[account], layer)
	if err := m.storeAccountPendingTxs(account, pending); err != nil {
		m.With().Error("failed to store account pending txs",
			layer, log.String("address", account.Short()), log.Err(err))
	}
}

func (m *DB) storeAccountPendingTxs(account types.Address, pending *pendingtxs.AccountPendingTxs) error {
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

func (m *DB) getAccountPendingTxs(addr types.Address) (*pendingtxs.AccountPendingTxs, error) {
	accountTxsBytes, err := m.unappliedTxs.Get(addr.Bytes())
	if err != nil && err != database.ErrNotFound {
		return nil, fmt.Errorf("failed to get mesh txs for account %s", addr.Short())
	}
	if err == database.ErrNotFound {
		return pendingtxs.NewAccountPendingTxs(), nil
	}
	var pending pendingtxs.AccountPendingTxs
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

// GetProjection returns projection of address
func (m *DB) GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error) {
	pending, err := m.getAccountPendingTxs(addr)
	if err != nil {
		return 0, 0, err
	}
	nonce, balance = pending.GetProjection(prevNonce, prevBalance)
	return nonce, balance, nil
}

type txGetter struct {
	missingIds map[types.TransactionID]struct{}
	txs        []*types.Transaction
	mesh       *DB
}

func (g *txGetter) get(id types.TransactionID) {
	t, err := g.mesh.GetTransaction(id)
	if err != nil {
		g.mesh.With().Warning("could not fetch tx", id, log.Err(err))
		g.missingIds[id] = struct{}{}
	} else {
		g.txs = append(g.txs, t)
	}
}

func newGetter(m *DB) *txGetter {
	return &txGetter{mesh: m, missingIds: make(map[types.TransactionID]struct{})}
}

// GetTransactions retrieves a list of txs by their id's
func (m *DB) GetTransactions(transactions []types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
	getter := newGetter(m)
	for _, id := range transactions {
		getter.get(id)
	}
	return getter.txs, getter.missingIds
}

// GetTransaction retrieves a tx by its id
func (m *DB) GetTransaction(id types.TransactionID) (*types.Transaction, error) {
	tBytes, err := m.transactions.Get(id[:])
	if err != nil {
		return nil, fmt.Errorf("could not find transaction in database %v err=%v", hex.EncodeToString(id[:]), err)
	}
	var dbTx DbTransaction
	err = types.BytesToInterface(tBytes, &dbTx)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal transaction: %v", err)
	}
	return dbTx.getTransaction(), nil
}

// GetTransactionsByDestination retrieves txs by destination and layer
func (m *DB) GetTransactionsByDestination(l types.LayerID, account types.Address) (txs []types.TransactionID) {
	it := m.transactions.Find(getTransactionDestKeyPrefix(l, account))
	for it.Next() {
		if it.Key() == nil {
			break
		}
		var a types.TransactionID
		err := types.BytesToInterface(it.Value(), &a)
		if err != nil {
			// log error
			break
		}
		txs = append(txs, a)
	}
	return
}

// GetTransactionsByOrigin retrieves txs by origin and layer
func (m *DB) GetTransactionsByOrigin(l types.LayerID, account types.Address) (txs []types.TransactionID) {
	it := m.transactions.Find(getTransactionOriginKeyPrefix(l, account))
	for it.Next() {
		if it.Key() == nil {
			break
		}
		var a types.TransactionID
		err := types.BytesToInterface(it.Value(), &a)
		if err != nil {
			// log error
			break
		}
		txs = append(txs, a)
	}
	return
}

// BlocksByValidity classifies a slice of blocks by validity
func (m *DB) BlocksByValidity(blocks []*types.Block) (validBlocks, invalidBlocks []*types.Block) {
	for _, b := range blocks {
		valid, err := m.ContextualValidity(b.ID())
		if err != nil {
			m.With().Error("could not get contextual validity by block", b.ID(), log.Err(err))
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
func (m *DB) ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	if layer == 0 || layer == 1 {
		v, err := m.LayerBlockIds(layer)
		if err != nil {
			m.With().Error("could not get layer block ids", layer, log.Err(err))
			return nil, err
		}

		mp := make(map[types.BlockID]struct{}, len(v))
		for _, blk := range v {
			mp[blk] = struct{}{}
		}

		return mp, nil
	}

	blockIds, err := m.LayerBlockIds(layer)
	if err != nil {
		return nil, err
	}

	validBlks := make(map[types.BlockID]struct{})

	cvErrors := make(map[string][]types.BlockID)
	cvErrorCount := 0
	for _, b := range blockIds {
		valid, err := m.ContextualValidity(b)
		if err != nil {
			cvErrors[err.Error()] = append(cvErrors[err.Error()], b)
			cvErrorCount++
		}

		if !valid {
			continue
		}

		validBlks[b] = struct{}{}
	}

	m.With().Info("count of contextually valid blocks in layer",
		layer,
		log.Int("count_valid", len(validBlks)),
		log.Int("count_error", cvErrorCount),
		log.Int("count_total", len(blockIds)))
	if cvErrorCount != 0 {
		m.With().Error("errors occurred getting contextual validity", layer,
			log.String("errors", fmt.Sprint(cvErrors)))
	}
	return validBlks, nil
}

// Persist persists an item v into the database using key as its id
func (m *DB) Persist(key []byte, v interface{}) error {
	buf, err := types.InterfaceToBytes(v)
	if err != nil {
		panic(err)
	}
	return m.general.Put(key, buf)
}

// Retrieve retrieves item by key into v
func (m *DB) Retrieve(key []byte, v interface{}) (interface{}, error) {
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

func (m *DB) cacheWarmUpFromTo(from types.LayerID, to types.LayerID) error {
	m.Info("warming up cache with layers %v to %v", from, to)
	for i := from; i < to; i++ {

		select {
		case <-m.exit:
			m.Info("shutdown during cache warm up")
			return nil
		default:
		}

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
	m.Info("done warming up cache")

	return nil
}
