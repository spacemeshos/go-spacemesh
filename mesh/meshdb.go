package mesh

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"sync/atomic"
	"time"
)

type layerHandler struct {
	ch           chan *Block
	layer        LayerID
	pendingCount int32
}

type meshDB struct {
	layers             database.DB
	blocks             database.DB
	contextualValidity database.DB //map blockId to contextualValidation state of block
	layerHandlers      map[LayerID]*layerHandler
	lhMutex            sync.Mutex
}

func NewMeshDb(layers database.DB, blocks database.DB, validity database.DB) *meshDB {
	ll := &meshDB{
		blocks:             blocks,
		layers:             layers,
		contextualValidity: validity,
		layerHandlers:      make(map[LayerID]*layerHandler),
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

func (m *meshDB) addBlock(block *Block) error {
	_, err := m.blocks.Get(block.ID().ToBytes())
	if err == nil {
		log.Debug("block ", block.ID(), " already exists in database")
		return errors.New("block " + string(block.ID()) + " already exists in database")
	}

	layerHandler := m.getLayerHandler(block.LayerIndex, 1)
	layerHandler.ch <- block
	return nil
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
	layerHandler := m.getLayerHandler(layer.index, int32(len(layer.blocks)))
	ids := make(map[BlockID]bool)
	for _, b := range layer.blocks {
		ids[b.Id] = true
	}

	//add blocks to mDB
	for _, b := range layer.blocks {
		layerHandler.ch <- b
	}

	w, err := blockIdsAsBytes(ids)
	if err != nil {
		//todo recover
		return errors.New("could not encode layer block ids")
	}

	m.layers.Put(layer.Index().ToBytes(), w)
	return nil
}

func (m *meshDB) updateLayerIds(block *Block) error {
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

func (m *meshDB) handleLayerBlocks(ll *layerHandler) {
	for {
		select {
		case block := <-ll.ch:
			atomic.AddInt32(&ll.pendingCount, -1)
			bytes, err := blockAsBytes(*block)
			if err != nil {
				log.Error("could not encode block")
				continue
			}
			if err := m.blocks.Put(block.ID().ToBytes(), bytes); err != nil {
				log.Error("could not add block to ", block, " database ", err)
				continue
			}
			m.updateLayerIds(block)
			m.tryDeleteHandler(ll) //try delete handler when done to avoid leak
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

//try delete layer Handler (deletes if pending pendingCount is 0)
func (m *meshDB) tryDeleteHandler(ll *layerHandler) {
	m.lhMutex.Lock()
	if atomic.LoadInt32(&ll.pendingCount) == 0 {
		delete(m.layerHandlers, ll.layer)
	}
	m.lhMutex.Unlock()
}

//returns the existing layer Handler (crates one if doesn't exist)
func (m *meshDB) getLayerHandler(index LayerID, counter int32) *layerHandler {
	m.lhMutex.Lock()
	defer m.lhMutex.Unlock()
	ll, found := m.layerHandlers[index]
	if !found {
		ll = &layerHandler{pendingCount: counter, ch: make(chan *Block)}
		m.layerHandlers[index] = ll
		go m.handleLayerBlocks(ll)
	} else {
		atomic.AddInt32(&ll.pendingCount, counter)
	}
	return ll
}
