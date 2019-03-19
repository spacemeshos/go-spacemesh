package mesh

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type layerMutex struct {
	m            sync.Mutex
	layerWorkers uint32
}

type meshDB struct {
	log.Log
	layers             database.DB
	blocks             database.DB
	transactions       database.DB
	contextualValidity database.DB //map blockId to contextualValidation state of block
	orphanBlocks       map[LayerID]map[BlockID]struct{}
	orphanBlockCount   int32
	layerMutex         map[LayerID]*layerMutex
	lhMutex            sync.Mutex
}

func NewMeshDB(layers, blocks, transactions, validity database.DB, log log.Log) *meshDB {
	ll := &meshDB{
		Log:                log,
		blocks:             blocks,
		layers:             layers,
		transactions:       transactions,
		contextualValidity: validity,
		orphanBlocks:       make(map[LayerID]map[BlockID]struct{}),
		layerMutex:         make(map[LayerID]*layerMutex),
	}
	return ll
}

func (m *meshDB) Close() {
	m.blocks.Close()
	m.layers.Close()
	m.contextualValidity.Close()
	m.transactions.Close()
}

func (m *meshDB) getLayer(index LayerID) (*Layer, error) {
	ids, err := m.layers.Get(index.ToBytes())
	if err != nil {
		return nil, fmt.Errorf("error getting layer %v from database ", index)
	}

	l := NewLayer(LayerID(index))
	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids for layer %v in database ", index)
	}

	blockIds, err := bytesToBlockIds(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	blocks, err := m.getLayerBlocks(blockIds)
	if err != nil {
		return nil, errors.New("could not get all blocks from database " + err.Error())
	}

	l.SetBlocks(blocks)

	return l, nil
}

// addBlock adds a new block to block DB and updates the correct layer with the new block
// if this is the first occurence of the layer a new layer object will be inserted into layerDB as well
func (m *meshDB) addBlock(block *Block) error {
	if _, err := m.getBlockHeaderBytes(block.ID()); err == nil {
		log.Debug("block ", block.ID(), " already exists in database")
		return errors.New("block " + string(block.ID()) + " already exists in database")
	}
	if err := m.writeBlock(block); err != nil {
		return err
	}
	return nil
}

func (m *meshDB) getBlockHeaderBytes(id BlockID) ([]byte, error) {
	b, err := m.blocks.Get(id.ToBytes())
	if err != nil {
		return nil, errors.New("could not find block in database")
	}
	return b, nil
}

func (m *meshDB) getBlock(id BlockID) (*Block, error) {

	b, err := m.getBlockHeaderBytes(id)
	if err != nil {
		return nil, err
	}

	blk, err := BytesAsBlockHeader(b)
	if err != nil {
		m.Error("some error 1")
		return nil, err
	}

	transactions, err := m.getTransactions(blk.TxIds)
	if err != nil {
		m.Error("some error 2")
		return nil, err
	}

	return blockFromHeaderAndTxs(blk, transactions), nil
}

func blockFromHeaderAndTxs(blk BlockHeader, transactions []*SerializableTransaction) *Block {
	block := Block{
		Id:         blk.Id,
		LayerIndex: blk.LayerIndex,
		MinerID:    blk.MinerID,
		Data:       blk.Data,
		Coin:       blk.Coin,
		Timestamp:  blk.Timestamp,
		BlockVotes: blk.BlockVotes,
		ViewEdges:  blk.ViewEdges,
		Txs:        transactions,
	}
	return &block
}

func (m *meshDB) getContextualValidity(id BlockID) (bool, error) {
	b, err := m.contextualValidity.Get(id.ToBytes())
	return b[0] == 1, err //bytes to bool
}

func (m *meshDB) setContextualValidity(id BlockID, valid bool) error {
	//todo implement
	//todo concurrency
	var v []byte
	if valid {
		v = TRUE
	}
	m.contextualValidity.Put(id.ToBytes(), v)
	return nil
}

func (m *meshDB) writeBlock(bl *Block) error {
	bytes, err := getBlockHeaderBytes(newBlockHeader(bl))
	if err != nil {
		return fmt.Errorf("could not encode bl")

	}
	if b, err := m.blocks.Get(bl.ID().ToBytes()); err == nil && b != nil {
		return fmt.Errorf("bl %v already in database ", bl.ID())
	}

	if err := m.blocks.Put(bl.ID().ToBytes(), bytes); err != nil {
		return fmt.Errorf("could not add bl to %v databacse %v", bl.ID(), err)
	}

	if err := m.writeTransactions(bl); err != nil {
		return fmt.Errorf("could not write transactions of block %v database %v", bl.ID(), err)
	}

	m.updateLayerWithBlock(bl)

	return nil
}

//todo this overwrites the previous value if it exists
func (m *meshDB) addLayer(layer *Layer) error {
	if len(layer.blocks) == 0 {
		m.layers.Put(layer.Index().ToBytes(), []byte{})
		return nil
	}

	//add blocks to mDB
	for _, bl := range layer.blocks {
		m.writeBlock(bl)
	}

	return nil
}

func (m *meshDB) updateLayerWithBlock(block *Block) error {
	lm := m.getLayerMutex(block.LayerIndex)
	defer m.endLayerWorker(block.LayerIndex)
	lm.m.Lock()
	defer lm.m.Unlock()
	ids, err := m.layers.Get(block.LayerIndex.ToBytes())
	var blockIds map[BlockID]bool
	if err != nil {
		//layer doesnt exist, need to insert new layer
		ids = []byte{}
		blockIds = make(map[BlockID]bool)
	} else {
		blockIds, err = bytesToBlockIds(ids)
		if err != nil {
			return errors.New("could not get all blocks from database ")
		}
	}
	m.Debug("added block %v to layer %v", block.ID(), block.LayerIndex)
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
	for k := range ids {
		block, err := m.getBlock(k)
		if err != nil {
			return nil, errors.New("could not retrieve block " + fmt.Sprint(k) + " " + err.Error())
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

//try delete layer Handler (deletes if pending pendingCount is 0)
func (m *meshDB) endLayerWorker(index LayerID) {
	m.lhMutex.Lock()
	defer m.lhMutex.Unlock()

	ll, found := m.layerMutex[index]
	if !found {
		panic("trying to double close layer mutex")
	}

	ll.layerWorkers--
	if ll.layerWorkers == 0 {
		delete(m.layerMutex, index)
	}
}

//returns the existing layer Handler (crates one if doesn't exist)
func (m *meshDB) getLayerMutex(index LayerID) *layerMutex {
	m.lhMutex.Lock()
	defer m.lhMutex.Unlock()
	ll, found := m.layerMutex[index]
	if !found {
		ll = &layerMutex{}
		m.layerMutex[index] = ll
	}
	ll.layerWorkers++
	return ll
}

func (m *meshDB) writeTransactions(block *Block) error {

	for _, t := range block.Txs {
		bytes, err := TransactionAsBytes(t)
		if err != nil {
			m.Error("could not write tx %v to database ")
		}

		//use AccountNonce Recipient Origin as key
		id := getTransactionId(t)
		m.transactions.Put(id, bytes)
		m.Debug("write tx %v to db", t)
	}

	return nil
}

func (m *meshDB) getTransactions(transactions [][]byte) ([]*SerializableTransaction, error) {
	var ts []*SerializableTransaction
	for _, id := range transactions {
		tBytes, err := m.getTransactionBytes(id)

		if err != nil {
			m.Error("error retrieving transaction from database ", err)
		}

		t, err := BytesAsTransaction(tBytes)

		if err != nil {
			m.Error("error deserializing transaction")
		}
		ts = append(ts, t)
	}

	return ts, nil
}

func getTransactionId(t *SerializableTransaction) []byte {
	return append(append(t.Origin.Bytes(), t.Recipient.Bytes()...), common.Uint64ToBytes(t.AccountNonce)...)
}

func (m *meshDB) getTransactionBytes(id []byte) ([]byte, error) {
	b, err := m.transactions.Get(id)
	if err != nil {
		return nil, errors.New("could not find transaction in database")
	}
	return b, nil
}
