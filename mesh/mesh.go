package mesh

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto/sha3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
)

const (
	layerSize     = 200
	Genesis       = 0
	GenesisId     = 420
)

var TRUE = []byte{1}
var FALSE = []byte{0}

type MeshValidator interface {
	HandleIncomingLayer(layer *block.Layer) (block.LayerID, block.LayerID)
	HandleLateBlock(bl *block.Block)
	ContextualValidity(id block.BlockID) bool
}

type StateUpdater interface {
	ApplyTransactions(layer block.LayerID, transactions Transactions) (uint32, error)
	ApplyRewards(layer block.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int)
}

type AtxDB interface {
	ProcessBlockATXs(block *block.Block)
}

type Mesh struct {
	log.Log
	mdb           *MeshDB
	AtxDB         AtxDB
	config        Config
	verifiedLayer block.LayerID
	latestLayer   block.LayerID
	lastSeenLayer block.LayerID
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
		mdb:      NewPersistentMeshDB(path, logger),
		config:   rewardConfig,
		AtxDB: atxdb,
	}

	return ll
}

func NewMemMesh(rewardConfig Config, mesh MeshValidator, state StateUpdater,atxdb AtxDB, logger log.Log) *Mesh {
	//todo add boot from disk
	ll := &Mesh{
		Log:      logger,
		tortoise: mesh,
		state:    state,
		done:     make(chan struct{}),
		mdb:      NewMemMeshDB(logger),
		config:   rewardConfig,
		AtxDB: atxdb,
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
		mdb:      db,
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

func SerializableTransaction2StateTransaction(tx *block.SerializableTransaction) *Transaction {
	price := big.Int{}
	price.SetBytes(tx.Price)

	amount := big.Int{}
	amount.SetBytes(tx.Amount)

	return NewTransaction(tx.AccountNonce, tx.Origin, *tx.Recipient, &amount, tx.GasLimit, &price)
}

func (m *Mesh) IsContexuallyValid(b block.BlockID) bool {
	//todo implement
	return true
}

func (m *Mesh) VerifiedLayer() block.LayerID {
	defer m.lvMutex.RUnlock()
	m.lvMutex.RLock()
	return m.verifiedLayer
}

func (m *Mesh) LatestLayer() block.LayerID {
	defer m.lkMutex.RUnlock()
	m.lkMutex.RLock()
	return m.latestLayer
}

func (m *Mesh) SetLatestLayer(idx block.LayerID) {
	defer m.lkMutex.Unlock()
	m.lkMutex.Lock()
	if idx > m.latestLayer {
		m.Info("set latest known layer to %v", idx)
		m.latestLayer = idx
	}
}

func (m *Mesh) ValidateLayer(lyr *block.Layer) {
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
		m.addAtxs(oldPbase, newPbase)
	}
}

func (m *Mesh) addAtxs(oldBase, newBase block.LayerID) {
	for i := oldBase; i < newBase; i++ {

		l, err := m.mdb.GetLayer(i)
		if err != nil || l == nil {
			m.Error("cannot find layer %v in db", i) //todo handle error
			return
		}
		for _, blk := range l.Blocks() {
			m.AtxDB.ProcessBlockATXs(blk)
		}

	}
}


func SortBlocks(blocks []*block.Block) []*block.Block {
	//not final sorting method, need to talk about this
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].ID() < blocks[j].ID() })
	return blocks
}

func (m *Mesh) ExtractUniqueOrderedTransactions(l *block.Layer) []*Transaction {
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

func (m *Mesh) PushTransactions(oldBase block.LayerID, newBase block.LayerID) {
	for i := oldBase; i < newBase; i++ {

		l, err := m.mdb.GetLayer(i)
		if err != nil || l == nil {
			m.Error("") //todo handle error
			return
		}

		merged := m.ExtractUniqueOrderedTransactions(l)
		x, err := m.state.ApplyTransactions(block.LayerID(i), merged)
		if err != nil {
			m.Log.Error("cannot apply transactions %v", err)
		}
		m.Log.Info("applied %v transactions in new pbase is %d apply result was %d", len(merged), newBase, x)
	}
}

//todo consider adding a boolean for layer validity instead error
func (m *Mesh) GetVerifiedLayer(i block.LayerID) (*block.Layer, error) {
	m.lMutex.RLock()
	if i > block.LayerID(m.verifiedLayer) {
		m.lMutex.RUnlock()
		m.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	m.lMutex.RUnlock()
	return m.mdb.GetLayer(i)
}

func (m *Mesh) GetLayer(i block.LayerID) (*block.Layer, error) {
	return m.mdb.GetLayer(i)
}

func (m *Mesh) GetLatestVerified() []block.BlockID{
	layer, err := m.mdb.GetLayer(m.VerifiedLayer())
	if err != nil {
		panic("got an error trying to read verified layer")
	}
	view := make([]block.BlockID, 0, len(layer.Blocks()))
	for _, blk := range layer.Blocks() {
		view = append(view, blk.Id)
	}
	return view
}

func (m *Mesh) AddBlock(blk *block.Block) error {
	m.Debug("add block %d", blk.ID())
	if err := m.mdb.AddBlock(blk); err != nil {
		m.Error("failed to add block %v  %v", blk.ID(), err)
		return err
	}
	m.SetLatestLayer(blk.Layer())
	//new block add to orphans
	m.handleOrphanBlocks(blk)
	//m.tortoise.HandleLateblock.Block(block) //why this? todo should be thread safe?
	return nil
}

//todo better thread safety
func (m *Mesh) handleOrphanBlocks(blk *block.Block) {
	m.orphMutex.Lock()
	defer m.orphMutex.Unlock()
	if _, ok := m.mdb.orphanBlocks[blk.Layer()]; !ok {
		m.mdb.orphanBlocks[blk.Layer()] = make(map[block.BlockID]struct{})
	}
	m.mdb.orphanBlocks[blk.Layer()][blk.ID()] = struct{}{}
	m.Info("Added block %d to orphans", blk.ID())
	atomic.AddInt32(&m.mdb.orphanBlockCount, 1)
	for _, b := range blk.ViewEdges {
		for _, layermap := range m.mdb.orphanBlocks {
			if _, has := layermap[b]; has {
				m.Log.Debug("delete block ", b, "from orphans")
				delete(layermap, b)
				atomic.AddInt32(&m.mdb.orphanBlockCount, -1)
				break
			}
		}
	}
}

func (m *Mesh) GetUnverifiedLayerBlocks(l block.LayerID) ([]block.BlockID, error) {
	x, err := m.mdb.layers.Get(l.ToBytes())
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not retrive layer = %d blocks ", l))
	}
	blockIds, err := block.BytesToBlockIds(x)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not desirialize layer to id array for layer %d ", l))
	}
	arr := make([]block.BlockID, 0, len(blockIds))
	for _, bid := range blockIds {
		arr = append(arr, bid)
	}
	return arr, nil
}

func (m *Mesh) GetOrphanBlocksBefore(l block.LayerID) ([]block.BlockID, error) {
	m.orphMutex.RLock()
	defer m.orphMutex.RUnlock()
	ids := map[block.BlockID]struct{}{}
	for key, val := range m.mdb.orphanBlocks {
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

	idArr := make([]block.BlockID, 0, len(ids))
	for i := range ids {
		idArr = append(idArr, i)
	}

	sort.Slice(idArr, func(i, j int) bool { return idArr[i] < idArr[j] })

	m.Info("orphans for layer %d are %v", l, idArr)
	return idArr, nil
}

func (m *Mesh) AccumulateRewards(rewardLayer block.LayerID, params Config) {
	l, err := m.mdb.GetLayer(rewardLayer)
	if err != nil || l == nil {
		m.Error("") //todo handle error
		return
	}

	ids := make(map[string]struct{})
	uq := make(map[string]struct{})

	// TODO: instead of the following code we need to validate the eligibility of each block individually using the
	//  proof included in each block
	for _, bl := range l.Blocks() {
		if _, found := ids[bl.MinerID.Key]; found {
			log.Error("two blocks found from same miner %v in layer %v", bl.MinerID, bl.LayerIndex)
			continue
		}
		ids[bl.MinerID.Key] = struct{}{}
		if uint32(len(bl.Txs)) < params.TxQuota {
			//todo: think of giving out reward for unique txs as well
			uq[bl.MinerID.Key] = struct{}{}
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
	m.state.ApplyRewards(block.LayerID(rewardLayer), ids, uq, bonusReward, diminishedReward)
	//todo: should miner id be sorted in a deterministic order prior to applying rewards?

}



func (m *Mesh) GetBlock(id block.BlockID) (*block.Block, error) {
	m.Debug("get block %d", id)
	return m.mdb.GetBlock(id)
}

func (m *Mesh) GetContextualValidity(id block.BlockID) (bool, error) {
	return m.mdb.getContextualValidity(id)
}

func (m *Mesh) Close() {
	m.Debug("closing mDB")
	m.mdb.Close()
}

func CreateGenesisBlock() *block.Block {
	bl := &block.Block{
		BlockHeader: block.BlockHeader{Id: block.BlockID(GenesisId),
			LayerIndex: 0,
			Data:       []byte("genesis")},
	}
	return bl
}

func GenesisLayer() *block.Layer {
	l := block.NewLayer(Genesis)
	l.AddBlock(CreateGenesisBlock())
	return l
}
