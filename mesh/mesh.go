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
	GetLayer(i LayerID) (*Layer, error)
	GetBlock(id BlockID) (*Block, error)
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

func NewMesh(newPeerCh chan Peer, newBlockCh chan *Block, layers database.DB, blocks database.DB) Mesh {
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

func (ll *LayersDB) GetLayer(i LayerID) (*Layer, error) {

	ll.lMutex.RLock()
	index := uint64(i)
	if index >= ll.layerCount {
		ll.lMutex.RUnlock()
		log.Debug("unknown layer ")
		return nil, errors.New("unknown layer ")
	}
	ll.lMutex.RUnlock()

	l, err := ll.layers.Get(new(big.Int).SetUint64(index).Bytes())
	if err != nil {
		return nil, errors.New("error getting layer from db ")
	}

	var ids []BlockID
	if _, err = xdr.Unmarshal(bytes.NewReader(l), &ids); err != nil {
		return nil, errors.New("error marshaling layer ")
	}

	blocks, err := ll.getLayerBlocks(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from db ")
	}

	return &Layer{index: LayerID(i), blocks: blocks}, nil
}

func (ll *LayersDB) getLayerBlocks(ids []BlockID) ([]*Block, error) {

	blocks := make([]*Block, 0, len(ids))
	for _, id := range ids {
		block, err := ll.GetBlock(id)
		if err != nil {
			return nil, errors.New("could not retrive block " + string(id))
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

func (ll *LayersDB) AddBlock(block Block) error {

	bytes, err := blockAsBytes(block)
	if err != nil {
		return errors.New("could not encode block")
	}

	err = ll.blocks.Put(new(big.Int).SetUint64(uint64(block.BlockId)).Bytes(), bytes)
	if err != nil {
		return errors.New("could not add block to database")
	}

	return nil
}

func (ll *LayersDB) GetBlock(id BlockID) (*Block, error) {
	b, err := ll.blocks.Get(new(big.Int).SetUint64(uint64(id)).Bytes())
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

	w, err := blockIdsAsBytes(layer)
	if err != nil {
		return errors.New("could not encode layer block ids")
	}

	ll.layers.Put(new(big.Int).SetUint64(uint64(layer.index)).Bytes(), w)

	//add blocks to db
	for _, b := range layer.blocks {
		if err = ll.AddBlock(*b); err != nil {
			log.Error("could not add block "+string(b.BlockId), err)
		}
	}

	ll.lcMutex.Lock()
	ll.layerCount++
	ll.lcMutex.Unlock()
	ll.lMutex.Unlock()

	return nil
}

func blockIdsAsBytes(layer *Layer) ([]byte, error) {
	ids := make([]BlockID, 0, len(layer.blocks))
	for _, b := range layer.blocks {
		ids = append(ids, b.BlockId)
	}
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &ids); err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return w.Bytes(), nil
}

func blockAsBytes(block Block) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &block); err != nil {
		return nil, errors.New("error marshalling block ids ")
	}
	return w.Bytes(), nil
}
