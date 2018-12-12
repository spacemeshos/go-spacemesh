package mesh

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type MeshDB interface {
	AddLayer(layer *Layer) error
	GetLayer(i LayerID) (*Layer, error)
	GetBlock(id BlockID) (*Block, error)
	AddBlock(block *Block) error
	Close()
}

type meshDB struct {
	layers     database.DB
	blocks     database.DB
	lMutex     sync.RWMutex
	llKeyMutex sync.Mutex
	layerLocks map[LayerID]sync.Mutex
}

func NewMeshDb(layers database.DB, blocks database.DB) MeshDB {
	ll := &meshDB{
		blocks:     blocks,
		layers:     layers,
		layerLocks: make(map[LayerID]sync.Mutex),
	}
	return ll
}

func (s *meshDB) Close() {
	s.blocks.Close()
	s.layers.Close()
}

func (ll *meshDB) GetLayer(index LayerID) (*Layer, error) {
	ids, err := ll.layers.Get(index.ToBytes())
	if err != nil {
		return nil, errors.New("error getting layer from database ")
	}

	blockIds, err := bytesToBlockIds(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	blocks, err := ll.getLayerBlocks(blockIds)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	return &Layer{index: LayerID(index), blocks: blocks}, nil
}

//todo fix concurrency for block
func (ll *meshDB) AddBlock(block *Block) error {

	_, err := ll.blocks.Get(block.Id().ToBytes())
	if err == nil {
		log.Debug("block ", block.Id(), " already exists in database")
		return errors.New("block " + string(block.Id()) + " already exists in database")
	}

	bytes, err := blockAsBytes(*block)
	if err != nil {
		return errors.New("could not encode block")
	}

	ll.llKeyMutex.Lock()
	layerLock := ll.getLayerLock(block.LayerIndex)
	layerLock.Lock()
	ll.llKeyMutex.Unlock()
	defer layerLock.Unlock()

	if err = ll.blocks.Put(block.Id().ToBytes(), bytes); err != nil {
		return errors.New("could not add block to database")
	}

	return ll.updateLayerIds(err, block)
}

func (ll *meshDB) getLayerLock(index LayerID) sync.Mutex {
	layerLock, found := ll.layerLocks[index]
	if !found {
		layerLock = sync.Mutex{}
		ll.layerLocks[index] = layerLock
	}
	return layerLock
}

func (ll *meshDB) GetBlock(id BlockID) (*Block, error) {
	b, err := ll.blocks.Get(id.ToBytes())
	if err != nil {
		return nil, errors.New("could not find block in database")
	}

	return bytesToBlock(b)
}

//todo this overwrites the previous value if it exists
func (ll *meshDB) AddLayer(layer *Layer) error {
	ll.llKeyMutex.Lock()
	layerLock := ll.getLayerLock(layer.index)
	layerLock.Lock()
	ll.llKeyMutex.Unlock()
	defer layerLock.Unlock()

	ids := make(map[BlockID]bool)
	for _, b := range layer.blocks {
		ids[b.id] = true
	}

	//add blocks to meshDb
	for _, b := range layer.blocks {
		bytes, err := blockAsBytes(*b)
		if err != nil {
			log.Error("problem serializing block ", b.Id(), err)
			delete(ids, b.Id()) //remove failed block from layer
			continue
		}

		err = ll.blocks.Put(b.Id().ToBytes(), bytes)
		if err != nil {
			log.Error("could not add block ", b.Id(), " to database ", err)
			delete(ids, b.Id()) //remove failed block from layer
		}

	}

	w, err := blockIdsAsBytes(ids)
	if err != nil {
		//todo recover
		return errors.New("could not encode layer block ids")
	}

	ll.layers.Put(layer.Index().ToBytes(), w)
	return nil
}

func (ll *meshDB) updateLayerIds(err error, block *Block) error {
	ids, err := ll.layers.Get(block.LayerIndex.ToBytes())
	blockIds, err := bytesToBlockIds(ids)
	if err != nil {
		return errors.New("could not get all blocks from database ")
	}
	blockIds[block.Id()] = true
	w, err := blockIdsAsBytes(blockIds)
	if err != nil {
		return errors.New("could not encode layer block ids")
	}
	ll.layers.Put(block.LayerIndex.ToBytes(), w)
	return nil
}

func (ll *meshDB) getLayerBlocks(ids map[BlockID]bool) ([]*Block, error) {

	blocks := make([]*Block, 0, len(ids))
	for k, _ := range ids {
		block, err := ll.GetBlock(k)
		if err != nil {
			return nil, errors.New("could not retrive block " + string(k))
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}
