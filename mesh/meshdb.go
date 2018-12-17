package mesh

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type meshDB struct {
	layers             database.DB
	blocks             database.DB
	contextualValidity database.DB //map blockId to contextualValidation state of block
	layerLocks         map[LayerID]sync.Mutex
	lMutex             sync.RWMutex
	llKeyMutex         sync.Mutex
}

func NewMeshDb(layers database.DB, blocks database.DB, validity database.DB) *meshDB {
	ll := &meshDB{
		blocks:             blocks,
		layers:             layers,
		contextualValidity: validity,
		layerLocks:         make(map[LayerID]sync.Mutex), //todo clean stale locks
	}
	return ll
}

func (m *meshDB) Close() {
	m.blocks.Close()
	m.layers.Close()
	m.contextualValidity.Close()
}

func (m *meshDB) getLayer(index LayerID) (*Layer, error) {
	ids, err := m.layers.Get(index.ToBytes())
	if err != nil {
		return nil, errors.New("error getting layer from database ")
	}

	blockIds, err := bytesToBlockIds(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	blocks, err := m.getLayerBlocks(blockIds)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	return &Layer{index: LayerID(index), blocks: blocks}, nil
}

//todo fix concurrency for block
//todo check syntactic validity
func (m *meshDB) addBlock(block *Block) error {

	_, err := m.blocks.Get(block.ID().ToBytes())
	if err == nil {
		log.Debug("block ", block.ID(), " already exists in database")
		return errors.New("block " + string(block.ID()) + " already exists in database")
	}

	bytes, err := blockAsBytes(*block)
	if err != nil {
		return errors.New("could not encode block")
	}

	m.llKeyMutex.Lock()
	layerLock := m.getLayerLock(block.LayerIndex)
	layerLock.Lock()
	m.llKeyMutex.Unlock()
	defer layerLock.Unlock()

	if err = m.blocks.Put(block.ID().ToBytes(), bytes); err != nil {
		return errors.New("could not add block to database")
	}

	return m.updateLayerIds(err, block)
}

func (m *meshDB) getLayerLock(index LayerID) sync.Mutex {
	layerLock, found := m.layerLocks[index]
	if !found {
		layerLock = sync.Mutex{}
		m.layerLocks[index] = layerLock
	}
	return layerLock
}

func (m *meshDB) getBlock(id BlockID) (*Block, error) {
	b, err := m.blocks.Get(id.ToBytes())
	if err != nil {
		return nil, errors.New("could not find block in database")
	}

	return bytesToBlock(b)
}

func (m *meshDB) getContextualValidity(id BlockID) (bool, error) {
	//todo implement
	return true, nil
}

func (m *meshDB) setContextualValidity(id BlockID, valid bool) error {
	//todo implement
	//todo concurrency
	m.contextualValidity.Put(id.ToBytes(), boolAsBytes(valid))
	return nil
}

//todo this overwrites the previous value if it exists
func (m *meshDB) addLayer(layer *Layer) error {
	m.llKeyMutex.Lock()
	layerLock := m.getLayerLock(layer.index)
	layerLock.Lock()
	m.llKeyMutex.Unlock()
	defer layerLock.Unlock()

	ids := make(map[BlockID]bool)
	for _, b := range layer.blocks {
		ids[b.Id] = true
	}

	//add blocks to mDB
	for _, b := range layer.blocks {
		bytes, err := blockAsBytes(*b)
		if err != nil {
			log.Error("problem serializing block ", b.ID(), err)
			delete(ids, b.ID()) //remove failed block from layer
			continue
		}

		err = m.blocks.Put(b.ID().ToBytes(), bytes)
		if err != nil {
			log.Error("could not add block ", b.ID(), " to database ", err)
			delete(ids, b.ID()) //remove failed block from layer
		}

	}

	w, err := blockIdsAsBytes(ids)
	if err != nil {
		//todo recover
		return errors.New("could not encode layer block ids")
	}

	m.layers.Put(layer.Index().ToBytes(), w)
	return nil
}

func (m *meshDB) updateLayerIds(err error, block *Block) error {
	ids, err := m.layers.Get(block.LayerIndex.ToBytes())
	blockIds, err := bytesToBlockIds(ids)
	if err != nil {
		return errors.New("could not get all blocks from database ")
	}
	blockIds[block.ID()] = true
	w, err := blockIdsAsBytes(blockIds)
	if err != nil {
		return errors.New("could not encode layer block ids")
	}
	m.layers.Put(block.LayerIndex.ToBytes(), w)
	return nil
}

func (m *meshDB) getLayerBlocks(ids map[BlockID]bool) ([]*Block, error) {

	blocks := make([]*Block, 0, len(ids))
	for k, _ := range ids {
		block, err := m.getBlock(k)
		if err != nil {
			return nil, errors.New("could not retrive block " + string(k))
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}
