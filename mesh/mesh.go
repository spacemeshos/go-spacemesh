package mesh

import (
	"crypto"
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type Mesh interface {
	AddLayer(layer *Layer) error
	GetLayer(i int) (*Layer, error)
	GetBlock(id BlockID) (*Block, error)
	LocalLayerCount() uint32
	LatestKnownLayer() uint32
	SetLatestKnownLayer(idx uint32)
	Close()
}

type Peer crypto.PublicKey

type LayersDB struct {
	layerCount       uint32
	latestKnownLayer uint32
	layers           []*Layer
	blocks           map[BlockID]*Block
	newPeerCh        chan Peer
	newBlockCh       chan Block
	exit             chan bool
	lMutex           sync.RWMutex
	lcMutex          sync.RWMutex
}

func NewLayers(newPeerCh chan Peer, newBlockCh chan Block) Mesh {
	ll := &LayersDB{
		0,
		0,
		nil,
		make(map[BlockID]*Block),
		newPeerCh,
		newBlockCh,
		make(chan bool),
		sync.RWMutex{},
		sync.RWMutex{}}
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

func (ll *LayersDB) GetLayer(i int) (*Layer, error) {
	ll.lMutex.RLock()
	if i < 1 {
		return nil, errors.New("index out of bounds len: " + string(len(ll.layers)) + " " + string(i))
	}
	if len(ll.layers) == 0 || i > len(ll.layers) {
		log.Debug("unknown layer ")
		return nil, errors.New("unknown layer ")
	}
	l := ll.layers[i-1]
	ll.lMutex.RUnlock()
	return l, nil
}

func (ll *LayersDB) GetBlock(id BlockID) (*Block, error) {
	return ll.blocks[id], nil
}

func (ll *LayersDB) LocalLayerCount() uint32 {
	//todo thread safety
	return ll.layerCount
}

func (ll *LayersDB) LatestKnownLayer() uint32 {
	//todo thread safety
	return ll.latestKnownLayer
}

func (ll *LayersDB) SetLatestKnownLayer(idx uint32) {
	ll.latestKnownLayer = idx
}

func (ll *LayersDB) AddLayer(layer *Layer) error {
	// validate idx
	// this is just an optimization
	if ll.layerCount != 0 && layer.Index()-1 > int(ll.layerCount) {
		return errors.New("can't add layer, missing previous layer ")
	} else if layer.Index() < int(ll.layerCount)-1 {
		return errors.New("layer already known ")
	}

	// validate blocks
	// todo send to tortoise
	ll.lMutex.Lock()
	//if is valid
	ll.layers = append(ll.layers, NewExistingLayer(uint32(layer.Index()), layer.blocks))
	//add blocks to db
	for _, b := range layer.blocks {
		ll.blocks[b.id] = b
	}
	ll.layerCount = uint32(len(ll.layers))
	ll.lMutex.Unlock()
	return nil
}
