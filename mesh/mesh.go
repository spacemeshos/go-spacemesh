package mesh

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
	"math/big"
	"sort"
	"sync"
	"sync/atomic"
)

const layerSize = 200
const cachedLayers = 50

var TRUE = []byte{1}
var FALSE = []byte{0}

/*type Mesh interface {
	AddLayer(layer *Layer) error
	GetVerifiedLayer(i LayerID) (*Layer, error)
	GetBlock(id BlockID) (*Block, error)
	AddBlock(block *Block) error
	GetContextualValidity(id BlockID) (bool, error)
	VerifiedLayer() uint32
	LatestLayer() uint32
	SetLatestLayer(idx uint32)
	GetOrphans() []BlockID
	Close()
}*/

type MeshValidator interface {
	HandleIncomingLayer(layer *Layer)
	HandleLateBlock(bl *Block)
	RegisterLayerCallback(func(layerId LayerID))
}

type StateUpdater interface {
	ApplyTransactions(layer state.LayerID, transactions state.Transactions) (uint32, error)
}

type Mesh struct {
	log.Log
	*meshDB
	verifiedLayer uint32
	latestLayer   uint32
	lastSeenLayer uint32
	lMutex        sync.RWMutex
	lkMutex       sync.RWMutex
	lcMutex       sync.RWMutex
	tortoise      MeshValidator
	state         StateUpdater
	orphMutex     sync.RWMutex
}

func NewMesh(layers, blocks, validity database.DB, mesh MeshValidator, state StateUpdater, logger log.Log) *Mesh {
	//todo add boot from disk
	ll := &Mesh{
		Log:      logger,
		tortoise: mesh,
		state:    state,
		meshDB:   NewMeshDB(layers, blocks, validity),
	}
	mesh.RegisterLayerCallback(ll.LayerCompleteCallback)
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

func (m *Mesh) VerifiedLayer() uint32 {
	return atomic.LoadUint32(&m.verifiedLayer)
}

func (m *Mesh) LatestReceivedLayer() uint32 { //maybe switch names with latestlayer?
	return atomic.LoadUint32(&m.lastSeenLayer)
}

func (m *Mesh) LatestLayer() uint32 {
	defer m.lkMutex.RUnlock()
	m.lkMutex.RLock()
	return m.latestLayer
}

func (m *Mesh) SetLatestLayer(idx uint32) {
	defer m.lkMutex.Unlock()
	m.lkMutex.Lock()
	if idx > m.latestLayer {
		m.Debug("set latest known layer to ", idx)
		m.latestLayer = idx
	}
}

func (m *Mesh) AddLayer(layer *Layer) error {
	m.lMutex.Lock()
	defer m.lMutex.Unlock()
	count := LayerID(m.LatestReceivedLayer())
	if count > layer.Index() {
		m.Debug("can't add layer ", layer.Index(), "(already exists)")
		return errors.New("can't add layer (already exists)")
	}

	if count+1 < layer.Index() {
		m.Debug("can't add layer", layer.Index(), " missing previous layers")
		return errors.New("can't add layer missing previous layers")
	}
	atomic.StoreUint32(&m.lastSeenLayer, uint32(layer.Index()))
	m.addLayer(layer)
	m.Log.With().Info("added layer", log.Uint32("id", uint32(layer.Index())), log.Int("num of blocks", len(layer.blocks)))
	m.SetLatestLayer(uint32(layer.Index()))
	return nil
}

func (m *Mesh) ValidateLayer(layer *Layer) {
	m.tortoise.HandleIncomingLayer(layer)
}

func (m *Mesh) LayerCompleteCallback(layerId LayerID) {
	m.Log.Info("layer %v is complete", layerId)
	l, err := m.getLayer(layerId)
	atomic.StoreUint32(&m.verifiedLayer, uint32(layerId))
	if err != nil {
		m.Log.Error("layer %v not found", layerId)
		return
		//panic("wtf - there was a race?")
	}

	txs := make([]*state.Transaction, 0, len(l.blocks))

	sort.Slice(l.blocks, func(i, j int) bool {
		return l.blocks[i].Id < l.blocks[j].Id
	}) //todo: blocks are now sorted... what does it mean? does the ordering degrade security?

	for _, b := range l.blocks {
		for _, tx := range b.Txs {
			//todo: think about these conversions.. are they needed?
			txs = append(txs, SerializableTransaction2StateTransaction(&tx))
		}
	}
	m.Log.Info("received %v txs in layer %v num of blocks: %v", len(txs), layerId, len(l.blocks))
	x, err := m.state.ApplyTransactions(state.LayerID(layerId), txs)
	if err != nil {
		m.Log.Error("cannot apply transactions %v", err)
	}
	m.Log.Info("applied %v transactions", x)

}

//todo consider adding a boolean for layer validity instead error
func (m *Mesh) GetVerifiedLayer(i LayerID) (*Layer, error) {
	m.lMutex.RLock()
	if i > LayerID(m.verifiedLayer) {
		m.lMutex.RUnlock()
		m.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	m.lMutex.RUnlock()
	return m.getLayer(i)
}

func (m *Mesh) GetLayer(i LayerID) (*Layer, error) {
	return m.getLayer(i)
}

func (m *Mesh) AddBlock(block *Block) error {
	log.Debug("add block ", block.ID())
	if err := m.addBlock(block); err != nil {
		m.Error("failed to add block ", block.ID(), " ", err)
		return err
	}
	m.SetLatestLayer(uint32(block.Layer()))
	//new block add to orphans
	m.handleOrphanBlocks(block)
	//m.tortoise.HandleLateBlock(block) //why this? todo should be thread safe?
	return nil
}

//todo better thread safety
func (m *Mesh) handleOrphanBlocks(block *Block) {
	m.orphMutex.Lock()
	defer m.orphMutex.Unlock()
	if _, ok := m.orphanBlocks[block.LayerIndex]; !ok {
		m.orphanBlocks[block.LayerIndex] = make(map[BlockID]struct{})
	}
	m.orphanBlocks[block.LayerIndex][block.Id] = struct{}{}
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

func (m *Mesh) GetOrphanBlocksByLayerId(layerId LayerID) []BlockID {
	m.orphMutex.RLock()
	defer m.orphMutex.RUnlock()
	if keys, has := m.orphanBlocks[layerId]; has {
		ids := make([]BlockID, 0, len(keys))

		for bid := range keys {
			ids = append(ids, bid)
		}
		return ids

	}
	return make([]BlockID, 0, 0)
}

//todo better thread safety
func (m *Mesh) GetOrphanBlocks() []BlockID {
	m.orphMutex.RLock()
	defer m.orphMutex.RUnlock()
	keys := make([]BlockID, 0, m.orphanBlockCount)
	for _, val := range m.orphanBlocks {
		for bid := range val {
			keys = append(keys, bid)
		}
	}

	return keys
}

//todo better thread safety
func (m *Mesh) GetOrphanBlocksExcept(l LayerID) []BlockID {
	m.orphMutex.RLock()
	defer m.orphMutex.RUnlock()
	keys := make([]BlockID, 0, m.orphanBlockCount)
	for key, val := range m.orphanBlocks {
		if key == l {
			continue
		}
		for bid := range val {
			keys = append(keys, bid)
		}
	}

	return keys
}

func (m *Mesh) GetBlock(id BlockID) (*Block, error) {
	m.Debug("get block ", id)
	return m.getBlock(id)
}

func (m *Mesh) GetContextualValidity(id BlockID) (bool, error) {
	return m.getContextualValidity(id)
}

func (m *Mesh) Close() {
	m.Debug("closing mDB")
	m.meshDB.Close()
}
