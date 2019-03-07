package mesh

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/layer"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
)

const layerSize = 200

var TRUE = []byte{1}
var FALSE = []byte{0}

type MeshValidator interface {
	HandleIncomingLayer(layer *Layer) (layer.Id, layer.Id)
	HandleLateBlock(bl *Block)
	ContextualValidity(id BlockID) bool
}

type StateUpdater interface {
	ApplyTransactions(layer layer.Id, transactions state.Transactions) (uint32, error)
	ApplyRewards(layer layer.Id, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int)
}

type Mesh struct {
	log.Log
	*meshDB
	rewardConfig  RewardConfig
	verifiedLayer layer.Id
	latestLayer   layer.Id
	lastSeenLayer layer.Id
	lMutex        sync.RWMutex
	lkMutex       sync.RWMutex
	lcMutex       sync.RWMutex
	lvMutex			sync.RWMutex
	tortoise      MeshValidator
	state         StateUpdater
	orphMutex     sync.RWMutex
	done          chan struct{}
}

func NewMesh(layers, blocks, validity database.DB, rewardConfig RewardConfig, mesh MeshValidator, state StateUpdater, logger log.Log) *Mesh {
	//todo add boot from disk
	ll := &Mesh{
		Log:          logger,
		tortoise:     mesh,
		state:        state,
		done:         make(chan struct{}),
		meshDB:       NewMeshDB(layers, blocks, validity, logger),
		rewardConfig: rewardConfig,
	}

	return ll
}

func SerializableTransaction2StateTransaction(tx *SerializableTransaction) *state.Transaction {
	price := big.Int{}
	price.SetBytes(tx.Price)

	amount := big.Int{}
	amount.SetBytes(tx.Amount)

	return state.NewTransaction(tx.AccountNonce, tx.Origin, *tx.Recipient, &amount, tx.GasLimit, &price)
}

func (m *Mesh) IsContexuallyValid(b BlockID) bool {
	//todo implement
	return true
}

func (m *Mesh) VerifiedLayer() layer.Id {
	defer m.lvMutex.RUnlock()
	m.lvMutex.RLock()
	return m.verifiedLayer
}

func (m *Mesh) LatestLayer() layer.Id {
	defer m.lkMutex.RUnlock()
	m.lkMutex.RLock()
	return m.latestLayer
}

func (m *Mesh) SetLatestLayer(idx layer.Id) {
	defer m.lkMutex.Unlock()
	m.lkMutex.Lock()
	if idx > m.latestLayer {
		m.Info("set latest known layer to %v", idx)
		m.latestLayer = idx
	}
}

func (m *Mesh) ValidateLayer(lyr *Layer) {
	m.Info("Validate layer %d", lyr.Index())

	if lyr.index >= m.rewardConfig.RewardMaturity {
		m.AccumulateRewards(lyr.index-m.rewardConfig.RewardMaturity, m.rewardConfig)
	}

	oldPbase, newPbase := m.tortoise.HandleIncomingLayer(lyr)
	m.lvMutex.Lock()
	m.verifiedLayer = lyr.Index()
	m.lvMutex.Unlock()
	if newPbase > oldPbase {
		m.PushTransactions(oldPbase, newPbase)
	}
}

func SortBlocks(blocks []*Block) []*Block {
	//not final sorting method, need to talk about this
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID() < blocks[j].ID() })
	return blocks
}

func (m *Mesh) ExtractUniqueOrderedTransactions(l *Layer) []*state.Transaction {
	txs := make([]*state.Transaction, 0, layerSize)
	sortedBlocks := SortBlocks(l.blocks)

	for _, b := range sortedBlocks {
		if !m.tortoise.ContextualValidity(b.ID()) {
			continue
		}
		for _, tx := range b.Txs {
			//todo: think about these conversions.. are they needed?
			txs = append(txs, SerializableTransaction2StateTransaction(&tx))
		}
	}

	return MergeDoubles(txs)
}

func (m *Mesh) PushTransactions(oldBase layer.Id, newBase layer.Id) {
	for i := oldBase; i < newBase; i++ {

		l, err := m.getLayer(i)
		if err != nil || l == nil {
			m.Error("") //todo handle error
			return
		}

		merged := m.ExtractUniqueOrderedTransactions(l)
		x, err := m.state.ApplyTransactions(layer.Id(i), merged)
		if err != nil {
			m.Log.Error("cannot apply transactions %v", err)
		}
		m.Log.Info("applied %v transactions in new pbase is %d apply result was %d", len(merged), newBase, x)
	}
}

//todo consider adding a boolean for layer validity instead error
func (m *Mesh) GetVerifiedLayer(i layer.Id) (*Layer, error) {
	m.lMutex.RLock()
	if i > layer.Id(m.verifiedLayer) {
		m.lMutex.RUnlock()
		m.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	m.lMutex.RUnlock()
	return m.getLayer(i)
}

func (m *Mesh) GetLayer(i layer.Id) (*Layer, error) {
	return m.getLayer(i)
}

func (m *Mesh) AddBlock(block *Block) error {
	m.Debug("add block %d", block.ID())
	if err := m.addBlock(block); err != nil {
		m.Error("failed to add block %v  %v", block.ID(), err)
		return err
	}
	m.SetLatestLayer(block.Layer())
	//new block add to orphans
	m.handleOrphanBlocks(block)
	//m.tortoise.HandleLateBlock(block) //why this? todo should be thread safe?
	return nil
}

//todo better thread safety
func (m *Mesh) handleOrphanBlocks(block *Block) {
	m.orphMutex.Lock()
	defer m.orphMutex.Unlock()
	if _, ok := m.orphanBlocks[block.Layer()]; !ok {
		m.orphanBlocks[block.Layer()] = make(map[BlockID]struct{})
	}
	m.orphanBlocks[block.Layer()][block.ID()] = struct{}{}
	m.Info("Added block %d to orphans", block.ID())
	atomic.AddInt32(&m.orphanBlockCount, 1)
	for _, b := range block.ViewEdges {
		for _, layermap := range m.orphanBlocks {
			if _, has := layermap[b]; has {
				m.Log.Debug("delete block ", b, "from orphans")
				delete(layermap, b)
				atomic.AddInt32(&m.orphanBlockCount, -1)
				break
			}
		}
	}
}

func (m *Mesh) GetUnverifiedLayerBlocks(l layer.Id) ([]BlockID, error) {
	x, err := m.meshDB.layers.Get(l.ToBytes())
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not retrive layer = %d blocks ", l))
	}
	blockIds, err := bytesToBlockIds(x)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not desirialize layer to id array for layer %d ", l))
	}
	arr := make([]BlockID, 0, len(blockIds))
	for bid := range blockIds {
		arr = append(arr, bid)
	}
	return arr, nil
}

func (m *Mesh) GetOrphanBlocksBefore(l layer.Id) ([]BlockID, error) {
	m.orphMutex.RLock()
	defer m.orphMutex.RUnlock()
	ids := map[BlockID]struct{}{}
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

	idArr := make([]BlockID, 0, len(ids))
	for i := range ids {
		idArr = append(idArr, i)
	}

	sort.Slice(idArr, func(i, j int) bool { return idArr[i] < idArr[j] })

	m.Info("orphans for layer %d are %v", l, idArr)
	return idArr, nil
}

func (m *Mesh) AccumulateRewards(rewardLayer layer.Id, params RewardConfig) {
	l, err := m.getLayer(rewardLayer)
	if err != nil || l == nil {
		m.Error("") //todo handle error
		return
	}

	ids := make(map[string]struct{})
	uq := make(map[string]struct{})

	//todo: check if block producer was eligible?
	for _, bl := range l.blocks {
		if _, found := ids[bl.MinerID]; found {
			log.Error("two blocks found from same miner %v in layer %v", bl.MinerID, bl.LayerIndex)
			continue
		}
		ids[bl.MinerID] = struct{}{}
		if uint32(len(bl.Txs)) < params.TxQuota {
			//todo: think of giving out reward for unique txs as well
			uq[bl.MinerID] = struct{}{}
		}
	}
	//accumulate all blocks rewards
	merged := m.ExtractUniqueOrderedTransactions(l)

	rewards := &big.Int{}
	processed := 0
	for _, tx := range merged {
		res := new(big.Int).Mul(tx.Price, params.SimpleTxCost)
		processed++
		rewards.Add(rewards, res)
	}

	layerReward := CalculateLayerReward(rewardLayer, params)
	rewards.Add(rewards, layerReward)

	numBlocks := big.NewInt(int64(len(l.blocks)))
	log.Info("fees reward: %v total processed %v total txs %v merged %v blocks: %v", rewards.Int64(), processed, len(merged), len(merged), numBlocks)

	bonusReward, diminishedReward := calculateActualRewards(rewards, numBlocks, params, len(uq))
	m.state.ApplyRewards(layer.Id(rewardLayer), ids, uq, bonusReward, diminishedReward)
	//todo: should miner id be sorted in a deterministic order prior to applying rewards?

}

func (m *Mesh) GetBlock(id BlockID) (*Block, error) {
	m.Debug("get block %d", id)
	return m.getBlock(id)
}

func (m *Mesh) GetContextualValidity(id BlockID) (bool, error) {
	return m.getContextualValidity(id)
}

func (m *Mesh) Close() {
	m.Debug("closing mDB")
	m.meshDB.Close()
}
