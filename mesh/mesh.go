package mesh

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"sync/atomic"
)

const layerSize = 200
const cachedLayers = 50

var TRUE = []byte{1}
var FALSE = []byte{0}

type Mesh interface {
	AddLayer(layer *Layer) error
	GetLayer(i LayerID) (*Layer, error)
	GetBlock(id BlockID) (*Block, error)
	AddBlock(block *Block) error
	GetContextualValidity(id BlockID) (bool, error)
	LocalLayer() uint32
	LatestLayer() uint32
	SetLatestLayer(idx uint32)
	GetOrphanBlocks() []BlockID
	Close()
}

type mesh struct {
	log.Log
	*meshDB
	localLayer  uint32
	latestLayer uint32
	lMutex      sync.RWMutex
	lkMutex     sync.RWMutex
	lcMutex     sync.RWMutex
	tortoise    Algorithm
	orphMutex   sync.RWMutex
}

func NewMesh(layers database.DB, blocks database.DB, validity database.DB, orphans database.DB,logger log.Log) Mesh {
	//todo add boot from disk
	ll := &mesh{
		Log:      logger,
		tortoise: NewAlgorithm(uint32(layerSize), uint32(cachedLayers)),
		meshDB:   NewMeshDb(layers, blocks, validity, orphans),
	}
	return ll
}

func (m *mesh) IsContexuallyValid(b BlockID) bool {
	//todo implement
	return true
}

func (m *mesh) LocalLayer() uint32 {
	return atomic.LoadUint32(&m.localLayer)
}

func (m *mesh) LatestLayer() uint32 {
	defer m.lkMutex.RUnlock()
	m.lkMutex.RLock()
	return m.latestLayer
}

func (m *mesh) SetLatestLayer(idx uint32) {
	defer m.lkMutex.Unlock()
	m.lkMutex.Lock()
	if idx > m.latestLayer {
		m.Debug("set latest known layer to ", idx)
		m.latestLayer = idx
	}
}

func (m *mesh) AddLayer(layer *Layer) error {
	m.lMutex.Lock()
	defer m.lMutex.Unlock()
	count := LayerID(m.LocalLayer())
	if count > layer.Index() {
		m.Debug("can't add layer ", layer.Index(), "(already exists)")
		return errors.New("can't add layer (already exists)")
	}

	if count+1 < layer.Index() {
		m.Debug("can't add layer", layer.Index(), " missing previous layers")
		return errors.New("can't add layer missing previous layers")
	}

	m.addLayer(layer)
	m.tortoise.HandleIncomingLayer(layer)
	atomic.AddUint32(&m.localLayer, 1)
	m.SetLatestLayer(uint32(layer.Index()))
	return nil
}

func (m *mesh) GetLayer(i LayerID) (*Layer, error) {
	m.lMutex.RLock()
	if i > LayerID(m.localLayer) {
		m.lMutex.RUnlock()
		m.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	m.lMutex.RUnlock()
	return m.getLayer(i)
}

func (m *mesh) AddBlock(block *Block) error {
	m.Debug("add block ", block.ID())
	if err := m.addBlock(block); err != nil {
		m.Error("failed to add block ", block.ID(), " ", err)
		return err
	}
	m.SetLatestLayer(uint32(block.Layer()))
	//new block add to orphans
	m.handleOrphanBlocks(block)
	m.tortoise.HandleLateBlock(block) //todo should be thread safe?
	return nil
}

//todo better thread safety
func (m *mesh) handleOrphanBlocks(block *Block) {
	m.orphanBlocks.Put(block.ID().ToBytes(), TRUE)
	atomic.AddInt32(&m.orphanBlockCount, 1)
	for b := range block.ViewEdges {
		blockId := b.ToBytes()
		if _, err := m.orphanBlocks.Get(blockId); err == nil {
			log.Debug("delete block ", blockId, "from orphans")
			m.orphanBlocks.Delete(blockId)
			atomic.AddInt32(&m.orphanBlockCount, -1)
		}
	}
}

//todo better thread safety
func (m *mesh) GetOrphanBlocks() []BlockID {
	m.orphMutex.Lock()
	defer m.orphMutex.Unlock()
	keys := make([]BlockID, 0, m.orphanBlockCount)
	iter := m.orphanBlocks.Iterator()
	for iter.Next() {
		// Remember that the contents of the returned slice should not be modified, and
		// only valid until the next call to Next.
		keys = append(keys, BlockID(BytesToUint32(iter.Key())))
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		log.Error("error iterating over orphans", err)
	}
	return keys
}

func (m *mesh) GetBlock(id BlockID) (*Block, error) {
	m.Debug("get block ", id)
	return m.getBlock(id)
}

func (m *mesh) GetContextualValidity(id BlockID) (bool, error) {
	return m.mDB.getContextualValidity(id)
}

func (m *mesh) Close() {
	m.Debug("closing mDB")
	m.mDB.Close()
}
