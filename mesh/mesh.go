package mesh

import (
	"bytes"
	"crypto"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"math/big"
	"sync"
	"sync/atomic"
	"time"
)

type Mesh interface {
	AddLayer(layer *Layer) error
	GetLayer(i uint64) (*Layer, error)
	GetBlock(id uint64) (*Block, error)
	LocalLayerCount() uint64
	LatestKnownLayer() uint64
	SetLatestKnownLayer(idx uint64)
	Close()
}

type Peer crypto.PublicKey

type LayersDB struct {
	layerCount       uint64
	latestKnownLayer uint64
	layers           database.DB
	blocks           database.DB
	newPeerCh        chan Peer
	newBlockCh       chan *Block
	exit             chan struct{}
	lMutex           sync.RWMutex
	lkMutex          sync.RWMutex
	lcMutex          sync.RWMutex
}

func NewLayers(newPeerCh chan Peer, newBlockCh chan *Block) Mesh {

	blocks := database.NewLevelDbStore("blocks", nil, nil)

	layers := database.NewLevelDbStore("layers", nil, nil)

	ll := &LayersDB{
		blocks:     blocks,
		layers:     layers,
		newPeerCh:  newPeerCh,
		newBlockCh: newBlockCh,
		exit:       make(chan struct{})}

	go ll.run()
	return ll
}

func (s *LayersDB) Close() {
	s.exit <- struct{}{}
	close(s.exit)
	close(s.newBlockCh)
	close(s.newPeerCh)
	s.blocks.Close()
	s.layers.Close()
}

func (s *LayersDB) run() {
	for {
		select {
		case <-s.exit:
			log.Debug("run stoped")
			return
		case <-s.newBlockCh:
		case <-s.newPeerCh:
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (ll *LayersDB) GetLayer(i uint64) (*Layer, error) {

	ll.lMutex.RLock()
	if i > ll.layerCount {
		ll.lMutex.RUnlock()
		log.Debug("unknown layer ")
		return nil, errors.New("unknown layer ")
	}
	ll.lMutex.RUnlock()

	l, err := ll.layers.Get(new(big.Int).SetUint64(i).Bytes())
	if err != nil {
		return nil, errors.New("error getting layer from db ")
	}

	var layer Layer
	_, err = xdr.Unmarshal(bytes.NewReader(l), &layer)
	if err != nil {
		return nil, errors.New("error marshaling layer ")
	}

	return &layer, nil
}

func (ll *LayersDB) GetBlock(id uint64) (*Block, error) {
	b, err := ll.blocks.Get(new(big.Int).SetUint64(id).Bytes())
	if err != nil {
		return nil, errors.New("could not find block in database")
	}
	var block Block
	if _, err = xdr.Unmarshal(bytes.NewReader(b), &block); err != nil {
		return nil, errors.New("could not unmarshal block")
	}
	return &block, nil
}

func (ll *LayersDB) LocalLayerCount() uint64 {
	defer ll.lcMutex.RUnlock()
	ll.lcMutex.RLock()
	return ll.layerCount
}

func (ll *LayersDB) LatestKnownLayer() uint64 {
	defer ll.lkMutex.RUnlock()
	ll.lkMutex.RLock()
	return atomic.LoadUint64(&ll.latestKnownLayer)
}

func (ll *LayersDB) SetLatestKnownLayer(idx uint64) {
	defer ll.lkMutex.Unlock()
	ll.lkMutex.Lock()
	ll.latestKnownLayer = idx
}

func (ll *LayersDB) AddLayer(layer *Layer) error {
	//validate idx

	if LayerID(ll.layerCount) > layer.Index() {
		return errors.New("can't add layer (already exists)")
	}

	if LayerID(ll.layerCount) < layer.Index() {
		return errors.New("can't add layer missing previous layers")
	}

	// validate blocks
	// todo send to tortoise
	ll.lMutex.Lock()
	//if is valid

	ids := make([]BlockID, 0, len(layer.blocks))
	for _, b := range layer.blocks {
		ids = append(ids, b.id)
	}

	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return errors.New("error marshalling block ids  ")
	}

	ll.layers.Put(new(big.Int).SetUint64(uint64(layer.index)).Bytes(), w.Bytes())

	//add blocks to db
	for _, b := range layer.blocks {
		var w bytes.Buffer
		if _, err := xdr.Marshal(&w, &b); err != nil {
			return errors.New("error marshalling blocks   ")
		}

		ll.blocks.Put(new(big.Int).SetUint64(uint64(b.id)).Bytes(), w.Bytes())
	}

	ll.lcMutex.Lock()
	ll.layerCount++
	ll.lcMutex.Unlock()
	ll.lMutex.Unlock()

	return nil
}
