package mesh

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"math/big"
	"sort"
	"sync"
)

const (
	layerSize   = 200
	Genesis     = types.LayerID(0)
	GenesisId   = 420
	TxCacheSize = 1000
)

var TRUE = []byte{1}
var FALSE = []byte{0}

type MeshValidator interface {
	HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID)
	HandleLateBlock(bl *types.Block)
}

type TxProcessor interface {
	ApplyTransactions(layer types.LayerID, transactions []*types.Transaction) (uint32, error)
	ApplyRewards(layer types.LayerID, miners []types.Address, underQuota map[types.Address]int, bonusReward, diminishedReward *big.Int)
	ValidateSignature(s types.Signed) (types.Address, error)
	AddressExists(addr types.Address) bool
}

type TxMemPoolInValidator interface {
	Invalidate(id types.TransactionId)
}

type AtxMemPoolInValidator interface {
	Invalidate(id types.AtxId)
}

type AtxDB interface {
	ProcessAtx(atx *types.ActivationTx)
	GetAtx(id types.AtxId) (*types.ActivationTxHeader, error)
	GetFullAtx(id types.AtxId) (*types.ActivationTx, error)
	SyntacticallyValidateAtx(atx *types.ActivationTx) error
}

type BlockBuilder interface {
	ValidateAndAddTxToPool(tx *types.Transaction, postValidationFunc func()) error
}

type Mesh struct {
	log.Log
	*MeshDB
	AtxDB
	TxProcessor
	blockBuilder   BlockBuilder
	txInvalidator  TxMemPoolInValidator
	atxInvalidator AtxMemPoolInValidator
	config         Config
	validatedLayer types.LayerID
	latestLayer    types.LayerID
	lMutex         sync.RWMutex
	lkMutex        sync.RWMutex
	lcMutex        sync.RWMutex
	lvMutex        sync.RWMutex
	tortoise       MeshValidator
	orphMutex      sync.RWMutex
	done           chan struct{}
}

func NewMesh(db *MeshDB, atxDb AtxDB, rewardConfig Config, mesh MeshValidator, txInvalidator TxMemPoolInValidator, atxInvalidator AtxMemPoolInValidator, pr TxProcessor, logger log.Log) *Mesh {
	//todo add boot from disk
	ll := &Mesh{
		Log:            logger,
		tortoise:       mesh,
		txInvalidator:  txInvalidator,
		atxInvalidator: atxInvalidator,
		TxProcessor:    pr,
		done:           make(chan struct{}),
		MeshDB:         db,
		config:         rewardConfig,
		AtxDB:          atxDb,
	}

	return ll
}

func (m *Mesh) SetBlockBuilder(blockBuilder BlockBuilder) {
	m.blockBuilder = blockBuilder
}

func (m *Mesh) ValidatedLayer() types.LayerID {
	defer m.lvMutex.RUnlock()
	m.lvMutex.RLock()
	return m.validatedLayer
}

// LatestLayer - returns the latest layer we saw from the network
func (m *Mesh) LatestLayer() types.LayerID {
	defer m.lkMutex.RUnlock()
	m.lkMutex.RLock()
	return m.latestLayer
}

func (m *Mesh) SetLatestLayer(idx types.LayerID) {
	defer m.lkMutex.Unlock()
	m.lkMutex.Lock()
	if idx > m.latestLayer {
		m.Info("set latest known layer to %v", idx)
		m.latestLayer = idx
	}
}

func (m *Mesh) GetLayer(index types.LayerID) (*types.Layer, error) {

	mBlocks, err := m.LayerBlocks(index)
	if err != nil {
		return nil, err
	}

	l := types.NewLayer(types.LayerID(index))
	l.SetBlocks(mBlocks)

	return l, nil
}

func (m *Mesh) ValidateLayer(lyr *types.Layer) {
	m.Info("Validate layer %d", lyr.Index())

	if lyr.Index() >= m.config.RewardMaturity {
		m.AccumulateRewards(lyr.Index()-m.config.RewardMaturity, m.config)
	}

	oldPbase, newPbase := m.tortoise.HandleIncomingLayer(lyr)
	m.lvMutex.Lock()
	m.validatedLayer = lyr.Index()
	m.lvMutex.Unlock()

	if newPbase > oldPbase {
		m.PushTransactions(oldPbase, newPbase)
	}
	m.Info("done validating layer %v", lyr.Index())
}

func (m *Mesh) ExtractUniqueTransactions(l *types.Layer) (validBlocks, invalidBlocks []*types.Transaction) {
	txids := map[bool]map[types.TransactionId]struct{}{
		true:  make(map[types.TransactionId]struct{}),
		false: make(map[types.TransactionId]struct{}),
	}

	for _, b := range l.Blocks() {
		valid, err := m.ContextualValidity(b.ID())
		if err != nil {
			m.With().Error("could not get contextual validity for block", log.BlockId(uint64(b.ID())), log.Err(err))
		}
		events.Publish(events.ValidBlock{Id: uint64(b.ID()), Valid: valid})
		if !valid {
			m.With().Warning("block not contextually valid", log.BlockId(uint64(b.ID()))) // TODO: do we want this log?
		}

		for _, id := range b.TxIds {
			txids[valid][id] = struct{}{}
		}
	}

	return m.getTxs(txids[true], l), m.getTxs(txids[false], l)
}

func (m *Mesh) getTxs(txIds map[types.TransactionId]struct{}, l *types.Layer) []*types.Transaction {
	txs, missing := m.GetTransactionsFromMap(txIds)
	if len(missing) != 0 {
		m.Panic("could not find transactions %v from layer %v", missing, l.Index())
	}
	return txs
}

func (m *Mesh) PushTransactions(oldBase, newBase types.LayerID) {
	for i := oldBase; i < newBase; i++ {
		l, err := m.GetLayer(i)
		if err != nil || l == nil {
			m.Error("could not get layer %v  !!!!!!!!!!!!!!!! %v ", i, err) //todo handle error
			return
		}

		validBlockTxs, invalidBlockTxs := m.ExtractUniqueTransactions(l)
		numFailedTxs, err := m.ApplyTransactions(i, validBlockTxs)
		if err != nil {
			m.With().Error("cannot apply transactions",
				log.LayerId(uint64(i)), log.Uint32("num_failed_txs", numFailedTxs), log.Err(err))
		}
		if err := m.removeFromMeshTxs(validBlockTxs, invalidBlockTxs, i); err != nil {
			m.With().Error("failed to remove from meshTxs", log.LayerId(uint64(i)), log.Err(err))
		}
		if m.blockBuilder != nil {
			for _, tx := range invalidBlockTxs {
				err = m.blockBuilder.ValidateAndAddTxToPool(tx, nil)
				// We ignore errors here, since they mean that the tx is no longer valid and we shouldn't re-add it
				if err == nil {
					m.With().Info("transaction from contextually invalid block re-added to mempool",
						log.TxId(tx.Id().ShortString()))
				}
			}
		}
		m.With().Info("applied transactions",
			log.Int("valid_block_txs", len(validBlockTxs)),
			log.Int("invalid_block_txs", len(invalidBlockTxs)),
			log.Uint64("new_base", uint64(newBase)),
			log.Uint32("num_failed_txs", numFailedTxs),
		)
	}
}

//todo consider adding a boolean for layer validity instead error
func (m *Mesh) GetVerifiedLayer(i types.LayerID) (*types.Layer, error) {
	m.lMutex.RLock()
	if i > types.LayerID(m.validatedLayer) {
		m.lMutex.RUnlock()
		m.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	m.lMutex.RUnlock()
	return m.GetLayer(i)
}

func (m *Mesh) GetLatestView() []types.BlockID {
	//todo: think about whether we want to use the most recent layer or the recent verified layer
	layer, err := m.GetLayer(m.LatestLayer())
	if err != nil {
		panic("got an error trying to read latest view")
	}
	view := make([]types.BlockID, 0, len(layer.Blocks()))
	for _, blk := range layer.Blocks() {
		view = append(view, blk.ID())
	}
	return view
}

func (m *Mesh) AddBlock(blk *types.Block) error {
	m.Debug("add block %d", blk.ID())
	if err := m.MeshDB.AddBlock(blk); err != nil {
		m.Warning("failed to add block %v  %v", blk.ID(), err)
		return err
	}
	m.SetLatestLayer(blk.Layer())
	//new block add to orphans
	m.handleOrphanBlocks(&blk.BlockHeader)

	//invalidate txs and atxs from pool
	m.invalidateFromPools(&blk.MiniBlock)
	return nil
}

func (m *Mesh) AddBlockWithTxs(blk *types.Block, txs []*types.Transaction, atxs []*types.ActivationTx) error {
	m.Debug("add block %d", blk.ID())

	events.Publish(events.NewBlock{Id: uint64(blk.ID()), Atx: blk.ATXID.ShortString(), Layer: uint64(blk.LayerIndex)})
	for _, t := range atxs {
		//todo this should return an error
		m.AtxDB.ProcessAtx(t)
	}

	err := m.writeTransactions(txs)
	if err != nil {
		return fmt.Errorf("could not write transactions of block %v database: %v", blk.ID(), err)
	}
	if err := m.addToMeshTxs(txs, blk.LayerIndex); err != nil {
		return fmt.Errorf("failed to add to meshTxs: %v", err)
	}

	if err := m.MeshDB.AddBlock(blk); err != nil && err != ErrAlreadyExist {
		m.With().Error("failed to add block", log.BlockId(uint64(blk.ID())), log.Err(err))
		return err
	}

	m.SetLatestLayer(blk.Layer())
	//new block add to orphans
	m.handleOrphanBlocks(&blk.BlockHeader)

	//invalidate txs and atxs from pool
	m.invalidateFromPools(&blk.MiniBlock)

	m.Debug("added block %d", blk.ID())
	return nil
}

func (m *Mesh) invalidateFromPools(blk *types.MiniBlock) {
	for _, id := range blk.TxIds {
		m.txInvalidator.Invalidate(id)
	}
	m.atxInvalidator.Invalidate(blk.ATXID)
	for _, id := range blk.AtxIds {
		m.atxInvalidator.Invalidate(id)
	}
}

//todo better thread safety
func (m *Mesh) handleOrphanBlocks(blk *types.BlockHeader) {
	m.orphMutex.Lock()
	defer m.orphMutex.Unlock()
	if _, ok := m.orphanBlocks[blk.Layer()]; !ok {
		m.orphanBlocks[blk.Layer()] = make(map[types.BlockID]struct{})
	}
	m.orphanBlocks[blk.Layer()][blk.ID()] = struct{}{}
	m.Info("Added block %d to orphans", blk.ID())
	for _, b := range blk.ViewEdges {
		for layerId, layermap := range m.orphanBlocks {
			if _, has := layermap[b]; has {
				m.Log.Debug("delete block ", b, "from orphans")
				delete(layermap, b)
				if len(layermap) == 0 {
					delete(m.orphanBlocks, layerId)
				}
				break
			}
		}
	}
}

func (m *Mesh) GetUnverifiedLayerBlocks(l types.LayerID) ([]types.BlockID, error) {
	x, err := m.layers.Get(l.ToBytes())
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not retrive layer = %d blocks ", l))
	}
	blockIds, err := types.BytesToBlockIds(x)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not desirialize layer to id array for layer %d ", l))
	}
	arr := make([]types.BlockID, 0, len(blockIds))
	for _, bid := range blockIds {
		arr = append(arr, bid)
	}
	return arr, nil
}

func (m *Mesh) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	m.orphMutex.RLock()
	defer m.orphMutex.RUnlock()
	ids := map[types.BlockID]struct{}{}
	for key, val := range m.orphanBlocks {
		if key < l {
			for bid := range val {
				ids[bid] = struct{}{}
			}
		}
	}

	blocks, err := m.GetUnverifiedLayerBlocks(l - 1)
	if err != nil {
		return nil, errors.New(fmt.Sprint("failed getting latest layer ", err))
	}

	//add last layer blocks
	for _, b := range blocks {
		ids[b] = struct{}{}
	}

	idArr := make([]types.BlockID, 0, len(ids))
	for i := range ids {
		idArr = append(idArr, i)
	}

	sort.Slice(idArr, func(i, j int) bool { return idArr[i] < idArr[j] })

	m.Info("orphans for layer %d are %v", l, idArr)
	return idArr, nil
}

func (m *Mesh) AccumulateRewards(rewardLayer types.LayerID, params Config) {
	l, err := m.GetLayer(rewardLayer)
	if err != nil || l == nil {
		m.Error("") //todo handle error
		return
	}

	ids := make([]types.Address, 0, len(l.Blocks()))
	uq := make(map[types.Address]int)

	// TODO: instead of the following code we need to validate the eligibility of each block individually using the
	//  proof included in each block
	for _, bl := range l.Blocks() {
		atx, err := m.AtxDB.GetAtx(bl.ATXID)
		if err != nil {
			m.With().Warning("Atx from block not found in db", log.Err(err), log.BlockId(uint64(bl.ID())), log.AtxId(bl.ATXID.ShortString()))
			continue
		}
		ids = append(ids, atx.Coinbase)
		if uint32(len(bl.TxIds)) < params.TxQuota {
			//todo: think of giving out reward for unique txs as well
			uq[atx.Coinbase] = uq[atx.Coinbase] + 1
		}
	}
	//accumulate all blocks rewards
	merged, _ := m.ExtractUniqueTransactions(l)

	rewards := &big.Int{}
	processed := 0
	for _, tx := range merged {
		res := new(big.Int).SetUint64(tx.GasPrice * params.SimpleTxCost.Uint64())
		processed++
		rewards.Add(rewards, res)
	}

	layerReward := CalculateLayerReward(rewardLayer, params)
	rewards.Add(rewards, layerReward)

	numBlocks := big.NewInt(int64(len(l.Blocks())))
	log.Info("fees reward: %v total processed %v total txs %v merged %v blocks: %v", rewards.Uint64(), processed, len(merged), len(merged), numBlocks)

	bonusReward, diminishedReward := calculateActualRewards(rewards, numBlocks, params, len(uq))
	m.ApplyRewards(types.LayerID(rewardLayer), ids, uq, bonusReward, diminishedReward)
	//todo: should miner id be sorted in a deterministic order prior to applying rewards?

}

var GenesisBlock = types.Block{
	MiniBlock: types.MiniBlock{
		BlockHeader: types.BlockHeader{Id: types.BlockID(GenesisId),
			LayerIndex: 0,
			Data:       []byte("genesis")},
	},
}

func GenesisLayer() *types.Layer {
	l := types.NewLayer(Genesis)
	l.AddBlock(&GenesisBlock)
	return l
}

func (m *Mesh) GetATXs(atxIds []types.AtxId) (map[types.AtxId]*types.ActivationTx, []types.AtxId) {
	var mIds []types.AtxId
	atxs := make(map[types.AtxId]*types.ActivationTx, len(atxIds))
	for _, id := range atxIds {
		t, err := m.GetFullAtx(id)
		if err != nil {
			m.Warning("could not get atx %v  from database, %v", id.ShortString(), err)
			mIds = append(mIds, id)
		} else {
			atxs[t.Id()] = t
		}
	}
	return atxs, mIds
}
