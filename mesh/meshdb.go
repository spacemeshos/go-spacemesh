package mesh

import (
	"container/list"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
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
	orphanBlocks       map[types.LayerID]map[types.BlockID]struct{}
	orphanBlockCount   int32
	layerMutex         map[types.LayerID]*layerMutex
	lhMutex            sync.Mutex
}

func NewPersistentMeshDB(path string, log log.Log) *MeshDB {
	bdb := database.NewLevelDbStore(path+"blocks", nil, nil)
	ldb := database.NewLevelDbStore(path+"layers", nil, nil)
	vdb := database.NewLevelDbStore(path+"validity", nil, nil)
	tdb := database.NewLevelDbStore(path+"transactions", nil, nil)
	ll := &MeshDB{
		Log:                log,
		blockCache:         NewBlockCache(10 * layerSize),
		blocks:             bdb,
		layers:             ldb,
		transactions:       tdb,
		contextualValidity: vdb,
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
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
		orphanBlocks:       make(map[types.LayerID]map[types.BlockID]struct{}),
		layerMutex:         make(map[types.LayerID]*layerMutex),
	}
	return ll
}

func (m *MeshDB) Close() {
	m.blocks.Close()
	m.layers.Close()
	m.transactions.Close()
	m.contextualValidity.Close()
}

// addBlock adds a new block to block DB and updates the correct layer with the new block
// if this is the first occurence of the layer a new layer object will be inserted into layerDB as well
func (m *MeshDB) AddBlock(block *types.Block) error {
	if _, err := m.getMiniBlockBytes(block.ID()); err == nil {
		log.Debug("block ", block.ID(), " already exists in database")
		return errors.New("block " + string(block.ID()) + " already exists in database")
	}
	if err := m.writeBlock(block); err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) GetMiniBlock(id types.BlockID) (*types.MiniBlock, error) {

	if blkh := m.blockCache.Get(id); blkh != nil {
		return blkh, nil
	}

	b, err := m.getMiniBlockBytes(id)
	if err != nil {
		return nil, err
	}
	mbk := &types.MiniBlock{}
	err = types.BytesToInterface(b, mbk)
	return mbk, err
}

func (m *MeshDB) LayerMiniBlocks(index types.LayerID) ([]*types.MiniBlock, error) {
	ids, err := m.layerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blocks := make([]*types.MiniBlock, 0, len(ids))
	for _, k := range ids {
		block, err := m.GetMiniBlock(k)
		if err != nil {
			return nil, errors.New("could not retrieve block " + fmt.Sprint(k) + " " + err.Error())
		}
		blocks = append(blocks, block)
	}

	return blocks, nil

}

func (m *MeshDB) LayerBlockIds(index types.LayerID) ([]types.BlockID, error) {

	idSet, err := m.layerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blockids := make([]types.BlockID, 0, len(idSet))
	for _, k := range idSet {
		blockids = append(blockids, k)
	}

	return blockids, nil
}

func (mc *MeshDB) ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.BlockHeader) error) error {
	stack := list.New()
	for b := range view {
		stack.PushFront(b)
	}
	set := make(map[types.BlockID]struct{})
	for b := stack.Front(); b != nil; b = stack.Front() {
		a := stack.Remove(stack.Front()).(types.BlockID)

		block, err := mc.GetMiniBlock(a)
		if err != nil {
			return err
		}

		//execute handler
		if err = blockHandler(&block.BlockHeader); err != nil {
			return err
		}

		//push children to bfs queue
		for _, id := range block.ViewEdges {
			if bChild, err := mc.GetMiniBlock(id); err != nil {
				return err
			} else {
				if bChild.LayerIndex >= layer { //dont traverse too deep
					if _, found := set[bChild.Id]; !found {
						set[bChild.Id] = struct{}{}
						stack.PushBack(bChild.Id)
					}
				}
			}
		}
	}
	return nil
}

func (m *MeshDB) layerBlockIds(index types.LayerID) ([]types.BlockID, error) {

	ids, err := m.layers.Get(index.ToBytes())
	if err != nil {
		return nil, fmt.Errorf("error getting layer %v from database ", index)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids for layer %v in database ", index)
	}

	idSet, err := types.BytesToBlockIds(ids)
	if err != nil {
		return nil, errors.New("could not get all blocks from database ")
	}

	return idSet, nil
}

func blockFromMiniAndTxs(blk *types.MiniBlock, transactions []*types.SerializableTransaction, atxs []*types.ActivationTx) *types.Block {
	block := types.Block{
		BlockHeader: blk.BlockHeader,
		Txs:         transactions,
		ATXs:        atxs,
	}
	return &block
}

func (m *MeshDB) getMiniBlockBytes(id types.BlockID) ([]byte, error) {
	return m.blocks.Get(id.ToBytes())
}

func (m *MeshDB) getContextualValidity(id types.BlockID) (bool, error) {
	b, err := m.contextualValidity.Get(id.ToBytes())
	return b[0] == 1, err //bytes to bool
}

func (m *MeshDB) setContextualValidity(id types.BlockID, valid bool) error {
	//todo implement
	//todo concurrency
	var v []byte
	if valid {
		v = TRUE
	}
	m.contextualValidity.Put(id.ToBytes(), v)
	return nil
}

func (m *MeshDB) writeBlock(bl *types.Block) error {

	txids, err := m.writeTransactions(bl)
	if err != nil {
		return fmt.Errorf("could not write transactions of block %v database %v", bl.ID(), err)
	}

	atxids := make([]types.AtxId, 0, len(bl.Txs))
	for _, t := range bl.ATXs {
		atxids = append(atxids, t.Id())
	}

	minblock := &types.MiniBlock{BlockHeader: bl.BlockHeader, TxIds: txids, ATXs: bl.ATXs}

	bytes, err := types.InterfaceToBytes(minblock)
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

func (m *MeshDB) updateLayerWithBlock(blk *types.Block) error {
	lm := m.getLayerMutex(blk.LayerIndex)
	defer m.endLayerWorker(blk.LayerIndex)
	lm.m.Lock()
	defer lm.m.Unlock()
	ids, err := m.layers.Get(blk.LayerIndex.ToBytes())
	var blockIds []types.BlockID
	if err != nil {
		//layer doesnt exist, need to insert new layer
		blockIds = make([]types.BlockID, 0, 1)
	} else {
		blockIds, err = types.BytesToBlockIds(ids)
		if err != nil {
			return errors.New("could not get all blocks from database ")
		}
	}
	m.Debug("added block %v to layer %v", blk.ID(), blk.LayerIndex)
	blockIds = append(blockIds, blk.ID())
	w, err := types.BlockIdsAsBytes(blockIds)
	if err != nil {
		return errors.New("could not encode layer blk ids")
	}
	m.layers.Put(blk.LayerIndex.ToBytes(), w)
	return nil
}

//try delete layer Handler (deletes if pending pendingCount is 0)
func (m *MeshDB) endLayerWorker(index types.LayerID) {
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
func (m *MeshDB) getLayerMutex(index types.LayerID) *layerMutex {
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

func (m *MeshDB) writeTransactions(blk *types.Block) ([]types.TransactionId, error) {
	txids := make([]types.TransactionId, 0, len(blk.Txs))
	for _, t := range blk.Txs {
		bytes, err := types.TransactionAsBytes(t)
		if err != nil {
			m.Error("could not marshall tx %v to bytes ", err)
			return nil, err
		}

		id := types.GetTransactionId(t)
		if err := m.transactions.Put(id[:], bytes); err != nil {
			m.Error("could not write tx %v to database ", hex.EncodeToString(id[:]), err)
			return nil, err
		}
		txids = append(txids, id)
		m.Debug("write tx %v to db", hex.EncodeToString(id[:]))
	}

	return txids, nil
}

func (m *MeshDB) GetTransactions(transactions []types.TransactionId) (
	map[types.TransactionId]*types.SerializableTransaction,
	[]types.TransactionId) {

	var mIds []types.TransactionId
	ts := make(map[types.TransactionId]*types.SerializableTransaction, len(transactions))
	for _, id := range transactions {
		t, err := m.GetTransaction(id)
		if err != nil {
			m.Error("could not fetch tx %v %v", hex.EncodeToString(id[:]), err)
			mIds = append(mIds, id)
		} else {
			ts[id] = t
		}
	}
	return ts, mIds
}

func (m *MeshDB) GetTransaction(id types.TransactionId) (*types.SerializableTransaction, error) {
	tBytes, err := m.transactions.Get(id[:])
	if err != nil {
		return nil, errors.New(fmt.Sprintf("could not find transaction in database %v", hex.EncodeToString(id[:])))
	}
	if err != nil {
		return nil, err
	}
	return types.BytesAsTransaction(tBytes)
}
