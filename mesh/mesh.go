package mesh

import (
	"crypto"
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"sync/atomic"
	"time"
)

type Peer crypto.PublicKey

const layerSize = 200
const cachedLayers = 50

type MeshData interface {
	AddLayer(layer *Layer) error //todo change this to add batch blocks
	GetLayer(i LayerID) (*Layer, error)
	GetBlock(id BlockID) (*Block, error)
	AddBlock(block *Block) error
	Close()
}

type Mesh interface {
	MeshData
	LocalLayerCount() uint64
	LatestKnownLayer() uint64
	SetLatestKnownLayer(idx uint64)
}

type ConsistentMesh struct {
	VerifiedLayerCount uint64
	latestKnownLayer   uint64
	meshData           MeshData
	newBlockCh         chan *Block
	exit               chan struct{}
	lMutex             sync.RWMutex
	lkMutex            sync.RWMutex
	tortoise           Algorithm
}

func (cm *ConsistentMesh) run() {

	for {
		select {
		case <-cm.exit:
			log.Debug("run stoped")
			return
		case b := <-cm.newBlockCh:
			cm.SetLatestKnownLayer(uint64(b.Layer()))
			cm.AddBlock(b)
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func NewMesh(newBlockCh chan *Block, layers database.DB, blocks database.DB) Mesh {
	ll := &ConsistentMesh{
		newBlockCh: newBlockCh,
		tortoise:   NewAlgorithm(uint32(layerSize), uint32(cachedLayers)),
		meshData:   NewMeshDb(layers, blocks),
		exit:       make(chan struct{})}

	go ll.run()

	return ll
}

func (cm *ConsistentMesh) LocalLayerCount() uint64 {
	return atomic.LoadUint64(&cm.VerifiedLayerCount)
}

func (cm *ConsistentMesh) LatestKnownLayer() uint64 {
	defer cm.lkMutex.RUnlock()
	cm.lkMutex.RLock()
	return cm.latestKnownLayer
}

//todo do this better
func (cm *ConsistentMesh) SetLatestKnownLayer(idx uint64) {
	defer cm.lkMutex.Unlock()
	cm.lkMutex.Lock()
	if idx > cm.latestKnownLayer {
		cm.latestKnownLayer = idx
	}
}

//todo thread safety
//this is for sync use sync adds layers one by one
//layer should include all layer blocks we know
func (cm *ConsistentMesh) AddLayer(layer *Layer) error {
	if LayerID(cm.VerifiedLayerCount) > layer.Index() {
		return errors.New("can't add layer (already exists)")
	}

	if LayerID(cm.VerifiedLayerCount) < layer.Index() {
		return errors.New("can't add layer missing previous layers")
	}

	cm.meshData.AddLayer(layer)

	cm.lMutex.Lock()
	defer cm.lMutex.Unlock()

	cm.tortoise.HandleIncomingLayer(layer)
	atomic.AddUint64(&cm.VerifiedLayerCount, 1)

	return nil
}

func (cm *ConsistentMesh) GetLayer(i LayerID) (*Layer, error) {

	cm.lMutex.RLock()
	if i >= LayerID(cm.VerifiedLayerCount) {
		cm.lMutex.RUnlock()
		return nil, errors.New("layer not verified yet")
	}
	cm.lMutex.RUnlock()

	return cm.meshData.GetLayer(i)
}

func (cm *ConsistentMesh) AddBlock(block *Block) error {
	if err := cm.meshData.AddBlock(block); err != nil {
		return err
	}
	cm.tortoise.HandleLateBlock(block)
	return nil
}

func (cm *ConsistentMesh) GetBlock(id BlockID) (*Block, error) {
	return cm.meshData.GetBlock(id)
}

func (cm *ConsistentMesh) Close() {
	cm.exit <- struct{}{}
	close(cm.exit)
	close(cm.newBlockCh)
	cm.meshData.Close()
}
