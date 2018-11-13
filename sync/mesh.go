package sync

import (
	"errors"
	"fmt"
	"sync"
)

type Block interface {
	Id() []byte
	Layer() int
}

type Layer interface {
	Index() int
	Blocks() []Block
	Hash() string
}

type Mesh interface {
	AddLayer(idx int, layer []Block) error
	GetLayer(i int) (Layer, error)
	GetBlock(layer int, id []byte) (Block, error)
	LocalLayerCount() int
	LatestKnownLayer() int
	SetLatestKnownLayer(idx int)
	Close()
}

type BlockValidator interface {
	CheckSyntacticValidity(block Block) bool
}

type BlockMock struct {
	id    []byte
	layer int
}

func (b *BlockMock) Id() []byte {
	return b.id
}

func (b *BlockMock) Layer() int {
	return b.layer
}

type LayerMock struct {
	idx    int
	blocks []Block
}

func (l *LayerMock) Index() int {
	return l.idx
}

func (l *LayerMock) Blocks() []Block {
	return l.blocks
}

func (l *LayerMock) Hash() string {
	return fmt.Sprintf("hash of layer %d", l.idx)
}

type LayersDB struct {
	layerCount       int
	latestKnownLayer int
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
			s.latestKnownLayer = max(s.latestKnownLayer, int(b.Layer()))
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

func (ll *LayersDB) GetBlock(layer int, id []byte) (Block, error) {
	return nil, nil
}

func (ll *LayersDB) LocalLayerCount() int {
	return ll.layerCount
}

func (ll *LayersDB) LatestKnownLayer() int {
	return ll.latestKnownLayer
}

func (ll *LayersDB) SetLatestKnownLayer(idx int) {
	ll.latestKnownLayer = idx
}

func (ll *LayersDB) AddLayer(idx int, layer []Block) error {
	// validate idx
	// this is just an optimization
	if ll.layerCount != 0 && idx-1 > int(ll.layerCount) {
		return errors.New("can't add layer, missing mesh ")
	} else if idx < int(ll.layerCount)-1 {
		return errors.New("layer already known ")
	}

	// validate blocks
	// todo tortoise the mesh
	// two tortoise simultaneously?

	ll.lMutex.Lock()
	//if is valid
	ll.layers = append(ll.layers, &LayerMock{idx, layer})
	ll.layerCount = len(ll.layers)
	ll.lMutex.Unlock()
	return nil
}
