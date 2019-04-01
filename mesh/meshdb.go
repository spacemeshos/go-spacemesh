package mesh

import (
	"container/list"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

type layerMutex struct {
	m            sync.Mutex
	layerWorkers uint32
}

type MeshDB struct {
	log.Log
	blockCache         blockCache
	layers             database.DB
	blocks             database.DB
	transactions       database.DB
	contextualValidity database.DB //map blockId to contextualValidation state of block
	orphanBlocks       map[LayerID]map[BlockID]struct{}
	orphanBlockCount   int32
	layerMutex         map[LayerID]*layerMutex
	lhMutex            sync.Mutex
}

func NewPersistentMeshDB(path string, log log.Log) *MeshDB {
	bdb := database.NewLevelDbStore(path+"blocks", nil, nil)
	ldb := database.NewLevelDbStore(path+"layers", nil, nil)
	vdb := database.NewLevelDbStore(path+"validity", nil, nil)
	tdb := database.NewLevelDbStore(path+"transactions", nil, nil)
	ll := &MeshDB{
		Log:                log,
		blockCache:         NewBlockCache(100 * layerSize),
		blocks:             bdb,
		layers:             ldb,
		transactions:       tdb,
		contextualValidity: vdb,
		orphanBlocks:       make(map[LayerID]map[BlockID]struct{}),
		layerMutex:         make(map[LayerID]*layerMutex),
	}
	return ll
}

func NewMemMeshDB(log log.Log) *MeshDB {
	db := database.NewMemDatabase()
	ll := &MeshDB{
		Log:                log,
		blockCache:         NewBlockCache(100 * layerSize),
		blocks:             db,
		layers:             db,
		contextualValidity: db,
		transactions:       db,
		orphanBlocks:       make(map[LayerID]map[BlockID]struct{}),
		layerMutex:         make(map[LayerID]*layerMutex),
	}
	return ll
}

func (m *MeshDB) Close() {
	m.blocks.Close()
	m.layers.Close()
	m.transactions.Close()
	m.contextualValidity.Close()
}

func (m *MeshDB) GetBlock(id BlockID) (*Block, error) {

	blk, err := m.GetMiniBlock(id)
	if err != nil {
		m.Error("could not retrieve block %v from database ", id)
		return nil, err
	}

	transactions, err := m.getTransactions(blk.TxIds)
	if err != nil {
		m.Error("could not retrieve block %v transactions from database ")
		return nil, err
	}

	return blockFromMiniAndTxs(blk, transactions), nil
}

// addBlock adds a new block to block DB and updates the correct layer with the new block
// if this is the first occurence of the layer a new layer object will be inserted into layerDB as well
func (m *MeshDB) AddBlock(block *Block) error {
	if _, err := m.getMiniBlockBytes(block.ID()); err == nil {
		log.Debug("block ", block.ID(), " already exists in database")
		return errors.New("block " + string(block.ID()) + " already exists in database")
	}
	if err := m.writeBlock(block); err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) GetMiniBlock(id BlockID) (*MiniBlock, error) {

	if blkh := m.blockCache.Get(id); blkh != nil {
		return blkh, nil
	}

	b, err := m.getMiniBlockBytes(id)
	if err != nil {
		return nil, err
	}

	return BytesAsMiniBlock(b)
}

//todo this overwrites the previous value if it exists
func (m *MeshDB) AddLayer(layer *Layer) error {
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

func (m *MeshDB) LayerBlockIds(index LayerID) ([]BlockID, error) {

	idSet, err := m.layerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blockids := make([]BlockID, 0, len(idSet))
	for k := range idSet {
		blockids = append(blockids, k)
	}

	return blockids, nil
}

func (m *MeshDB) GetLayer(index LayerID) (*Layer, error) {
	bids, err := m.layerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blocks, err := m.getLayerBlocks(bids)
	if err != nil {
		return nil, err
	}

	l := NewLayer(LayerID(index))
	l.SetBlocks(blocks)

	return l, nil
}

func (mc *MeshDB) ForBlockInView(view map[BlockID]struct{}, layer LayerID, blockHandler func(block *BlockHeader), errHandler func(err error)) {
	stack := list.New()
	for b := range view {
		stack.PushFront(b)
	}
	set := make(map[BlockID]struct{})
	for b := stack.Front(); b != nil; b = stack.Front() {
		a := stack.Remove(stack.Front()).(BlockID)

		block, err := mc.GetMiniBlock(a)
		if err != nil {
			errHandler(err)
		}

		//execute handler
		blockHandler(&block.BlockHeader)

		//push children to bfs queue
		for _, id := range block.ViewEdges {
			bChild, err := mc.GetMiniBlock(id)
			if err != nil {
				errHandler(err)
			}
			if bChild.LayerIndex >= layer { //dont traverse too deep
				if _, found := set[bChild.Id]; !found {
					set[bChild.Id] = struct{}{}
					stack.PushBack(bChild.Id)
				}
			}
		}
	}
	return
}

func (m *MeshDB) layerBlockIds(index LayerID) (map[BlockID]bool, error) {

	ids, err := m.layers.Get(index.ToBytes())
	if err != nil {
		return nil, fmt.Errorf("error getting layer %v from database ", index)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids for layer %v in database ", index)
	}

	idSet, err := bytesToBlockIds(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	return idSet, nil
}

func blockFromMiniAndTxs(blk *MiniBlock, transactions []*SerializableTransaction) *Block {
	block := Block{
		BlockHeader: blk.BlockHeader,
		Txs:         transactions,
	}
	return &block
}

func (m *MeshDB) getMiniBlockBytes(id BlockID) ([]byte, error) {
	b, err := m.blocks.Get(id.ToBytes())
	if err != nil {
		return nil, errors.New("could not find block in database")
	}
	return b, nil
}

func (m *MeshDB) getContextualValidity(id BlockID) (bool, error) {
	b, err := m.contextualValidity.Get(id.ToBytes())
	return b[0] == 1, err //bytes to bool
}

func (m *MeshDB) setContextualValidity(id BlockID, valid bool) error {
	//todo implement
	//todo concurrency
	var v []byte
	if valid {
		v = TRUE
	}
	m.contextualValidity.Put(id.ToBytes(), v)
	return nil
}

func (m *MeshDB) writeBlock(bl *Block) error {

	txids, err := m.writeTransactions(bl)
	if err != nil {
		return fmt.Errorf("could not write transactions of block %v database %v", bl.ID(), err)
	}

	minblock := &MiniBlock{*toBlockHeader(bl), txids}
	minblock.ATXs = bl.ATXs
	bytes, err := getMiniBlockBytes(minblock)
	if err != nil {
		return fmt.Errorf("could not encode bl")
	}

	if err := m.blocks.Put(bl.ID().ToBytes(), bytes); err != nil {
		return fmt.Errorf("could not add bl to %v databacse %v", bl.ID(), err)
	}

	m.updateLayerWithBlock(bl)

	m.blockCache.put(minblock)

	return nil
}

func (m *MeshDB) updateLayerWithBlock(block *Block) error {
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

func (m *MeshDB) getLayerBlocks(ids map[BlockID]bool) ([]*Block, error) {

	blocks := make([]*Block, 0, len(ids))
	for k := range ids {
		block, err := m.GetBlock(k)
		if err != nil {
			return nil, errors.New("could not retrieve block " + fmt.Sprint(k) + " " + err.Error())
		}
		blocks = append(blocks, block)
	}

	return blocks, nil
}

//try delete layer Handler (deletes if pending pendingCount is 0)
func (m *MeshDB) endLayerWorker(index LayerID) {
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
func (m *MeshDB) getLayerMutex(index LayerID) *layerMutex {
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

func (m *MeshDB) writeTransactions(block *Block) ([]TransactionId, error) {
	txids := make([]TransactionId, 0, len(block.Txs))
	for _, t := range block.Txs {
		bytes, err := TransactionAsBytes(t)
		if err != nil {
			m.Error("could not write tx %v to database ", err)
			return nil, err
		}

		id := getTransactionId(t)
		if err := m.transactions.Put(id, bytes); err != nil {
			m.Error("could not write tx %v to database ", err)
			return nil, err
		}
		txids = append(txids, id)
		m.Debug("write tx %v to db", t)
	}

	return txids, nil
}

func (m *MeshDB) getTransactions(transactions []TransactionId) ([]*SerializableTransaction, error) {
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

//todo standardized transaction id across project
//todo replace panic
func getTransactionId(t *SerializableTransaction) TransactionId {
	tx, err := TransactionAsBytes(t)
	if err != nil {
		panic("could not Serialize transaction")
	}

	return crypto.Sha256(tx)
}

func (m *MeshDB) getTransactionBytes(id []byte) ([]byte, error) {
	b, err := m.transactions.Get(id)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not find transaction in database %v", id))
	}
	return b, nil
}
