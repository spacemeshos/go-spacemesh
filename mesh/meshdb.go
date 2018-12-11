package mesh

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type MeshDB struct {
	layers     database.DB
	blocks     database.DB
	lMutex     sync.RWMutex
	llKeyMutex sync.Mutex
	layerLocks map[LayerID]sync.Mutex
}

func NewMeshDb(layers database.DB, blocks database.DB) MeshData {
	ll := &MeshDB{
		blocks:     blocks,
		layers:     layers,
		layerLocks: make(map[LayerID]sync.Mutex),
	}
	return ll
}
func (s *MeshDB) Close() {
	s.blocks.Close()
	s.layers.Close()
}

func (ll *MeshDB) GetLayer(index LayerID) (*Layer, error) {
	ids, err := ll.layers.Get(index.ToBytes())
	if err != nil {
		return nil, errors.New("error getting layer from meshData ")
	}

	blockIds, err := bytesToBlockIds(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from meshData ")
	}

	blocks, err := ll.getLayerBlocks(blockIds)
	if err != nil {
		return nil, errors.New("could not get all blocks from meshData ")
	}

	return &Layer{index: LayerID(index), blocks: blocks}, nil
}

//todo fix concurrency for block
func (ll *MeshDB) AddBlock(block *Block) error {

	_, err := ll.blocks.Get(block.BlockId.ToBytes())
	if err == nil {
		log.Debug("block ", block.BlockId, " already exists in database")
		return errors.New("block " + string(block.BlockId) + " already exists in database")
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

	if err = ll.blocks.Put(block.BlockId.ToBytes(), bytes); err != nil {
		return errors.New("could not add block to database")
	}

	return ll.updateLayerIds(err, block)
}

func (ll *MeshDB) getLayerLock(index LayerID) sync.Mutex {
	layerLock, found := ll.layerLocks[index]
	if !found {
		layerLock = sync.Mutex{}
		ll.layerLocks[index] = layerLock
	}
	return layerLock
}

func (ll *MeshDB) GetBlock(id BlockID) (*Block, error) {
	b, err := ll.blocks.Get(id.ToBytes())
	if err != nil {
		return nil, errors.New("could not find block in database")
	}

	return bytesToBlock(b)
}

//todo this overwrites the previous value if it exists
func (ll *MeshDB) AddLayer(layer *Layer) error {
	ll.llKeyMutex.Lock()
	layerLock := ll.getLayerLock(layer.index)
	layerLock.Lock()
	ll.llKeyMutex.Unlock()
	defer layerLock.Unlock()

	ids := make(map[BlockID]bool)
	for _, b := range layer.blocks {
		ids[b.BlockId] = true
	}

	w, err := blockIdsAsBytes(ids)
	if err != nil {
		return errors.New("could not encode layer block ids")
	}

	//add blocks to meshData
	for _, b := range layer.blocks {

		bytes, err := blockAsBytes(*b)
		if err != nil {
			//todo error handling
			log.Debug("problem adding block to db ", err)
		}

		err = ll.blocks.Put(b.BlockId.ToBytes(), bytes)
		if err != nil {
			//todo error handling
			log.Debug("could not add block to database ", err)
		}

	}

	ll.layers.Put(layer.Index().ToBytes(), w)
	return nil
}

func (ll *MeshDB) updateLayerIds(err error, block *Block) error {
	ids, err := ll.layers.Get(block.LayerIndex.ToBytes())
	blockIds, err := bytesToBlockIds(ids)
	if err != nil {
		return errors.New("could not get all blocks from meshData ")
	}
	blockIds[block.BlockId] = true
	w, err := blockIdsAsBytes(blockIds)
	if err != nil {
		return errors.New("could not encode layer block ids")
	}
	ll.layers.Put(block.LayerIndex.ToBytes(), w)
	return nil
}

func (ll *MeshDB) getLayerBlocks(ids map[BlockID]bool) ([]*Block, error) {

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
