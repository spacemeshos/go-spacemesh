package mesh

import (
	"crypto"
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"sync/atomic"
)

type Peer crypto.PublicKey

const layerSize = 200
const cachedLayers = 50

type Mesh interface {
	MeshDB
	LocalLayerCount() uint32
	LatestKnownLayer() uint32
	SetLatestKnownLayer(idx uint32)
}

type mesh struct {
	localLayerCount  uint32
	latestKnownLayer uint32
	meshDb           MeshDB
	lMutex           sync.RWMutex
	lkMutex          sync.RWMutex
	lcMutex          sync.RWMutex
	tortoise         Algorithm
}

func NewMesh(layers database.DB, blocks database.DB) Mesh {
	//todo add boot from disk
	ll := &mesh{
		tortoise: NewAlgorithm(uint32(layerSize), uint32(cachedLayers)),
		meshDb:   NewMeshDb(layers, blocks),
	}
	return ll
}

func (cm *mesh) LocalLayerCount() uint32 {
	return atomic.LoadUint32(&cm.localLayerCount)
}

func (cm *mesh) LatestKnownLayer() uint32 {
	defer cm.lkMutex.RUnlock()
	cm.lkMutex.RLock()
	return cm.latestKnownLayer
}

func (cm *mesh) SetLatestKnownLayer(idx uint32) {
	defer cm.lkMutex.Unlock()
	cm.lkMutex.Lock()
	if idx > cm.latestKnownLayer {
		log.Debug("set latest known layer to ", idx)
		cm.latestKnownLayer = idx
	}
}

func (cm *mesh) AddLayer(layer *Layer) error {
	cm.lMutex.Lock()
	defer cm.lMutex.Unlock()
	count := LayerID(cm.LocalLayerCount())
	if count > layer.Index() {
		log.Debug("can't add layer ", layer.Index(), "(already exists)")
		return errors.New("can't add layer (already exists)")
	}

	if count < layer.Index() {
		log.Debug("can't add layer", layer.Index(), " missing previous layers")
		return errors.New("can't add layer missing previous layers")
	}

	cm.meshDb.AddLayer(layer)
	cm.tortoise.HandleIncomingLayer(layer)
	atomic.AddUint32(&cm.localLayerCount, 1)
	return nil
}

func (cm *mesh) GetLayer(i LayerID) (*Layer, error) {
	cm.lMutex.RLock()
	if i >= LayerID(cm.localLayerCount) {
		cm.lMutex.RUnlock()
		log.Debug("failed to get layer  ", i, " layer not verified yet")
		return nil, errors.New("layer not verified yet")
	}
	cm.lMutex.RUnlock()
	return cm.meshDb.GetLayer(i)
}

func (cm *mesh) AddBlock(block *Block) error {
	log.Debug("add block ", block.ID())
	if err := cm.meshDb.AddBlock(block); err != nil {
		log.Debug("failed to add block ", block.ID(), " ", err)
		return err
	}
	cm.SetLatestKnownLayer(uint32(block.Layer()))
	cm.tortoise.HandleLateBlock(block)
	return nil
}

func (cm *mesh) GetBlock(id BlockID) (*Block, error) {
	log.Debug("get block ", id)
	return cm.meshDb.GetBlock(id)
}

func (cm *mesh) Close() {
	log.Debug("closing db")
	cm.meshDb.Close()
}
