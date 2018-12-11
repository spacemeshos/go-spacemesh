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

type Mesh interface {
	MeshDB
	LocalLayerCount() uint64
	LatestKnownLayer() uint64
	SetLatestKnownLayer(idx uint64)
}

type mesh struct {
	VerifiedLayerCount uint64
	latestKnownLayer   uint64
	meshDb             MeshDB
	lMutex             sync.RWMutex
	lkMutex            sync.RWMutex
	tortoise           Algorithm
}

func NewMesh(layers database.DB, blocks database.DB) Mesh {
	//todo add boot from disk
	ll := &mesh{
		tortoise: NewAlgorithm(uint32(layerSize), uint32(cachedLayers)),
		meshDb:   NewMeshDb(layers, blocks),
	}
	return ll
}

func (cm *mesh) LocalLayerCount() uint64 {
	return atomic.LoadUint64(&cm.VerifiedLayerCount)
}

func (cm *mesh) LatestKnownLayer() uint64 {
	defer cm.lkMutex.RUnlock()
	cm.lkMutex.RLock()
	return cm.latestKnownLayer
}

//todo do this better
func (cm *mesh) SetLatestKnownLayer(idx uint64) {
	defer cm.lkMutex.Unlock()
	cm.lkMutex.Lock()
	if idx > cm.latestKnownLayer {
		cm.latestKnownLayer = idx
	}
}

//this is for sync use sync adds layers one by one
//layer should include all layer blocks we know
func (cm *mesh) AddLayer(layer *Layer) error {
	if LayerID(cm.VerifiedLayerCount) > layer.Index() {
		return errors.New("can't add layer (already exists)")
	}

	if LayerID(cm.VerifiedLayerCount) < layer.Index() {
		return errors.New("can't add layer missing previous layers")
	}

	cm.meshDb.AddLayer(layer)

	cm.lMutex.Lock()
	defer cm.lMutex.Unlock()

	cm.tortoise.HandleIncomingLayer(layer)
	atomic.AddUint64(&cm.VerifiedLayerCount, 1)

	return nil
}

func (cm *mesh) GetLayer(i LayerID) (*Layer, error) {

	cm.lMutex.RLock()
	if i >= LayerID(cm.VerifiedLayerCount) {
		cm.lMutex.RUnlock()
		return nil, errors.New("layer not verified yet")
	}
	cm.lMutex.RUnlock()

	return cm.meshDb.GetLayer(i)
}

func (cm *mesh) AddBlock(block *Block) error {
	if err := cm.meshDb.AddBlock(block); err != nil {
		return err
	}
	cm.tortoise.HandleLateBlock(block)
	return nil
}

func (cm *mesh) GetBlock(id BlockID) (*Block, error) {
	return cm.meshDb.GetBlock(id)
}

func (cm *mesh) Close() {
	cm.meshDb.Close()
}

//todo test this
//todo better name
//todo add configuration options
//todo logs

func Listen(mesh Mesh, newBlockCh chan *Block, exit chan struct{}) (run func(), kill func()) {

	run = func() {

		for {
			select {
			case <-exit:
				log.Debug("run stoped")
				return
			case b := <-newBlockCh:
				mesh.SetLatestKnownLayer(uint64(b.Layer()))
				mesh.AddBlock(b)
			default:
				time.Sleep(100 * time.Millisecond)
			}

		}
	}

	kill = func() {

		exit <- struct{}{}
		close(exit)
		close(newBlockCh)
	}

	return run, kill
}
