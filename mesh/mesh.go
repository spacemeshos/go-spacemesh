package mesh

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto/sha3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"github.com/spacemeshos/go-spacemesh/types"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
)

const (
	layerSize = 200
	Genesis   = types.LayerID(0)
	GenesisId = 420
)

var TRUE = []byte{1}
var FALSE = []byte{0}

type MeshValidator interface {
	HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID)
	HandleLateBlock(bl *types.Block)
	ContextualValidity(id types.BlockID) bool
}

type StateUpdater interface {
	ApplyTransactions(layer types.LayerID, transactions Transactions) (uint32, error)
	ApplyRewards(layer types.LayerID, miners []string, underQuota map[string]int, bonusReward, diminishedReward *big.Int)
}

type AtxDB interface {
	ProcessBlockATXs(block *types.Block)
	GetAtx(id types.AtxId) (*types.ActivationTx, error)
}

type Mesh struct {
	log.Log
	*MeshDB
	AtxDB         AtxDB
	config        Config
	verifiedLayer types.LayerID
	latestLayer   types.LayerID
	lastSeenLayer types.LayerID
	lMutex        sync.RWMutex
	lkMutex       sync.RWMutex
	lcMutex       sync.RWMutex
	lvMutex       sync.RWMutex
	tortoise      MeshValidator
	state         StateUpdater
	orphMutex     sync.RWMutex
	done          chan struct{}
}

func NewPersistentMesh(path string, rewardConfig Config, mesh MeshValidator, state StateUpdater, atxdb AtxDB, logger log.Log) *Mesh {
	//todo add boot from disk
	ll := &Mesh{
		Log:      logger,
		tortoise: mesh,
		state:    state,
		done:     make(chan struct{}),
		MeshDB:   NewPersistentMeshDB(path, logger),
		config:   rewardConfig,
		AtxDB:    atxdb,
	}

	return ll
}

func NewMemMesh(rewardConfig Config, mesh MeshValidator, state StateUpdater, atxdb AtxDB, logger log.Log) *Mesh {
	//todo add boot from disk
	ll := &Mesh{
		Log:      logger,
		tortoise: mesh,
		state:    state,
		done:     make(chan struct{}),
		MeshDB:   NewMemMeshDB(logger),
		config:   rewardConfig,
		AtxDB:    atxdb,
	}

	return ll
}

func NewMesh(db *MeshDB, atxDb AtxDB, rewardConfig Config, mesh MeshValidator, state StateUpdater, logger log.Log) *Mesh {
	//todo add boot from disk
	ll := &Mesh{
		Log:      logger,
		tortoise: mesh,
		state:    state,
		done:     make(chan struct{}),
		MeshDB:   db,
		config:   rewardConfig,
		AtxDB:    atxDb,
	}

	return ll
}

//todo: this object should be splitted into two parts: one is the actual value serialized into trie, and an containig obj with caches
type Transaction struct {
	AccountNonce uint64
	Price        *big.Int
	GasLimit     uint64
	Recipient    *address.Address
	Origin       address.Address //todo: remove this, should be calculated from sig.
	Amount       *big.Int
	Payload      []byte

	//todo: add signatures

	hash *common.Hash
}

func (tx *Transaction) Hash() common.Hash {
	if tx.hash == nil {
		hash := rlpHash(tx)
		tx.hash = &hash
	}
	return *tx.hash
}

type Transactions []*Transaction

func NewTransaction(nonce uint64, origin address.Address, destination address.Address,
	amount *big.Int, gasLimit uint64, gasPrice *big.Int) *Transaction {
	return &Transaction{
		AccountNonce: nonce,
		Origin:       origin,
		Recipient:    &destination,
		Amount:       amount,
		GasLimit:     gasLimit,
		Price:        gasPrice,
		hash:         nil,
		Payload:      nil,
	}
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

func SerializableTransaction2StateTransaction(tx *types.SerializableTransaction) *Transaction {
	price := big.Int{}
	price.SetBytes(tx.Price)

	amount := big.Int{}
	amount.SetBytes(tx.Amount)

	return NewTransaction(tx.AccountNonce, tx.Origin, *tx.Recipient, &amount, tx.GasLimit, &price)
}

func (m *Mesh) IsContexuallyValid(b types.BlockID) bool {
	//todo implement
	return true
}

func (m *Mesh) VerifiedLayer() types.LayerID {
	defer m.lvMutex.RUnlock()
	m.lvMutex.RLock()
	return m.verifiedLayer
}

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

func (m *Mesh) ValidateLayer(lyr *types.Layer) {
	m.Info("Validate layer %d", lyr.Index())

	if lyr.Index() >= m.config.RewardMaturity {
		m.AccumulateRewards(lyr.Index()-m.config.RewardMaturity, m.config)
	}

	oldPbase, newPbase := m.tortoise.HandleIncomingLayer(lyr)
	m.lvMutex.Lock()
	m.verifiedLayer = lyr.Index()
	m.lvMutex.Unlock()

	if newPbase > oldPbase {
		m.PushTransactions(oldPbase, newPbase)
	}
}

func (m *Mesh) addAtxs(l *types.Layer) {
	for _, blk := range l.Blocks() {
		m.AtxDB.ProcessBlockATXs(blk)
	}
}

func SortBlocks(blocks []*types.Block) []*types.Block {
	//not final sorting method, need to talk about this
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID() < blocks[j].ID() })
	return blocks
}

func (m *Mesh) ExtractUniqueOrderedTransactions(l *types.Layer) []*Transaction {
	txs := make([]*Transaction, 0, layerSize)
	sortedBlocks := SortBlocks(l.Blocks())

	for _, b := range sortedBlocks {
		if !m.tortoise.ContextualValidity(b.ID()) {
			continue
		}
		for _, tx := range b.Txs {
			//todo: think about these conversions.. are they needed?
			txs = append(txs, SerializableTransaction2StateTransaction(tx))
		}
	}

	return MergeDoubles(txs)
}

func (m *Mesh) PushTransactions(oldBase types.LayerID, newBase types.LayerID) {
	for i := oldBase; i < newBase; i++ {

		l, err := m.GetLayer(i)
		if err != nil || l == nil {
			m.Error("") //todo handle error
			return
		}

		merged := m.ExtractUniqueOrderedTransactions(l)
		x, err := m.state.ApplyTransactions(types.LayerID(i), merged)
		if err != nil {
			m.Log.Error("cannot apply transactions %v", err)
		}
		m.Log.Info("applied %v transactions in new pbase is %d apply result was %d", len(merged), newBase, x)
	}
}

//todo consider adding a boolean for layer validity instead error
func (m *Mesh) GetVerifiedLayer(i types.LayerID) (*types.Layer, error) {
	m.lMutex.RLock()
	if i > types.LayerID(m.verifiedLayer) {
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
		view = append(view, blk.Id)
	}
	return view
}

func (m *Mesh) AddBlock(blk *types.Block) error {
	m.Debug("add block %d", blk.ID())
	if err := m.MeshDB.AddBlock(blk); err != nil {
		m.Error("failed to add block %v  %v", blk.ID(), err)
		return err
	}
	m.AtxDB.ProcessBlockATXs(blk)
	m.SetLatestLayer(blk.Layer())
	//new block add to orphans
	m.handleOrphanBlocks(blk)
	//m.tortoise.HandleLateblock.Block(block) //why this? todo should be thread safe?
	return nil
}

//todo better thread safety
func (m *Mesh) handleOrphanBlocks(blk *types.Block) {
	m.orphMutex.Lock()
	defer m.orphMutex.Unlock()
	if _, ok := m.orphanBlocks[blk.Layer()]; !ok {
		m.orphanBlocks[blk.Layer()] = make(map[types.BlockID]struct{})
	}
	m.orphanBlocks[blk.Layer()][blk.ID()] = struct{}{}
	m.Info("Added block %d to orphans", blk.ID())
	atomic.AddInt32(&m.orphanBlockCount, 1)
	for _, b := range blk.ViewEdges {
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

	ids := make([]string, 0, len(l.Blocks()))
	uq := make(map[string]int)

	// TODO: instead of the following code we need to validate the eligibility of each block individually using the
	//  proof included in each block
	for _, bl := range l.Blocks() {
		atx, err := m.AtxDB.GetAtx(bl.ATXID)
		if err != nil {
			m.Log.Error("Atx not found %v layer %v block %v", bl.ATXID.String()[:5], bl.LayerIndex, bl.Id)
			continue
		}
		ids = append(ids, atx.NodeId.Key)
		if uint32(len(bl.Txs)) < params.TxQuota {
			//todo: think of giving out reward for unique txs as well
			uq[bl.MinerID.Key] = uq[bl.MinerID.Key] + 1
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

	numBlocks := big.NewInt(int64(len(l.Blocks())))
	log.Info("fees reward: %v total processed %v total txs %v merged %v blocks: %v", rewards.Int64(), processed, len(merged), len(merged), numBlocks)

	bonusReward, diminishedReward := calculateActualRewards(rewards, numBlocks, params, len(uq))
	m.state.ApplyRewards(types.LayerID(rewardLayer), ids, uq, bonusReward, diminishedReward)
	//todo: should miner id be sorted in a deterministic order prior to applying rewards?

}

func (m *Mesh) GetContextualValidity(id types.BlockID) (bool, error) {
	return m.getContextualValidity(id)
}

func (m *Mesh) Close() {
	m.Debug("closing mDB")
	m.MeshDB.Close()
}

func CreateGenesisBlock() *types.Block {
	bl := &types.Block{
		BlockHeader: types.BlockHeader{Id: types.BlockID(GenesisId),
			LayerIndex: 0,
			Data:       []byte("genesis")},
	}
	return bl
}

func GenesisLayer() *types.Layer {
	l := types.NewLayer(Genesis)
	l.AddBlock(CreateGenesisBlock())
	return l
}
