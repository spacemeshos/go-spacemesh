package sync

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
)

type Mesh interface {
	AddLayer(layer Layer) error
	GetLayer(i int) (Layer, error)
	GetBlock(layer int, id uint32) (Block, error)
	LocalLayerCount() uint32
	LatestKnownLayer() uint32
	SetLatestKnownLayer(idx uint32)
	Close()
}

type BlockValidator interface {
	CheckSyntacticValidity(block Block) bool
}

type LayersDB struct {
	layerCount       uint32
	latestKnownLayer uint32
	layers           []Layer
	newPeerCh        chan Peer
	newBlockCh       chan Block
	exit             chan bool
	lMutex           sync.Mutex
	lkMutex          sync.Mutex
	lcMutex          sync.Mutex
}

func NewLayers(newPeerCh chan Peer, newBlockCh chan Block) Mesh {
	ll := &LayersDB{0, 0, nil, newPeerCh, newBlockCh, make(chan bool), sync.Mutex{}, sync.Mutex{}, sync.Mutex{}}
	go ll.run()
	return ll
}

func (s *LayersDB) Close() {
	s.exit <- true
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *LayersDB) run() {
	for {
		select {
		case <-s.newPeerCh:
			//	do something on new peer ??????????
		case b := <-s.newBlockCh:
			s.lkMutex.Lock()
			s.latestKnownLayer = uint32(max(int(s.latestKnownLayer), int(b.Layer())))
			s.lkMutex.Unlock()
		case <-s.exit:
			fmt.Println("run stoped")
			return
		default:
		}
	}
}

func (ll *LayersDB) GetLayer(i int) (Layer, error) {
	// TODO validate index
	if i >= len(ll.layers) {
		return nil, errors.New("index to high")
	}
	return ll.layers[i], nil
}

func (ll *LayersDB) GetBlock(layer int, id uint32) (Block, error) {
	return nil, nil
}

func (ll *LayersDB) LocalLayerCount() uint32 {
	return ll.layerCount
}

func (ll *LayersDB) LatestKnownLayer() uint32 {
	return ll.latestKnownLayer
}

func (ll *LayersDB) SetLatestKnownLayer(idx uint32) {
	ll.latestKnownLayer = idx
}

func (ll *LayersDB) AddLayer(layer Layer) error {
	// validate idx
	// this is just an optimization
	if ll.layerCount != 0 && layer.Index()-1 > int(ll.layerCount) {
		return errors.New("can't add layer, missing mesh ")
	} else if layer.Index() < int(ll.layerCount)-1 {
		return errors.New("layer already known ")
	}

	// validate blocks
	// todo tortoise the mesh
	// two tortoise simultaneously?

	ll.lMutex.Lock()
	//if is valid
	ll.layers = append(ll.layers, mesh.NewExistingLayer(uint32(layer.Index()), nil))
	ll.layerCount = uint32(len(ll.layers))
	ll.lMutex.Unlock()
	return nil
}
