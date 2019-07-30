package mesh

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"time"
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
		blockCache:         NewBlockCache(100 * layerSize),
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

func (m *MeshDB) AddBlock(bl *types.Block) error {
	defer m.Time(time.Now(), fmt.Sprintf("AddBlock (%v)", bl.Id))
	if _, err := m.getMiniBlockBytes(bl.ID()); err == nil {
		return errors.New(fmt.Sprintf("block %v already exists in database", bl.ID()))
	}
	if err := m.writeBlock(bl); err != nil {
		return err
	}
	return nil
}

func (m *MeshDB) GetBlock(id types.BlockID) (*types.Block, error) {
	t := time.Now()
	if blkh := m.blockCache.Get(id); blkh != nil {
		m.Log.Info("running GetBlock (%v) took %v read from cache: %v",id, time.Since(t), true)
		return blkh, nil
	}

	b, err := m.getMiniBlockBytes(id)
	if err != nil {
		return nil, err
	}
	mbk := &types.Block{}
	err = types.BytesToInterface(b, mbk)
	m.Log.Info("running GetBlock (%v) took %v read from cache: %v",id, time.Since(t), false)
	return mbk, err
}

func (m *MeshDB) LayerBlocks(index types.LayerID) ([]*types.Block, error) {
	ids, err := m.layerBlockIds(index)
	if err != nil {
		return nil, err
	}

	blocks := make([]*types.Block, 0, len(ids))
	for _, k := range ids {
		block, err := m.GetBlock(k)
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

type blockQueue struct {
	l []*types.Block
}

func (s *blockQueue) Push(b *types.Block) {
	s.l = append(s.l, b)
}

func (s *blockQueue) Pop() *types.Block {
	b := s.l[0]
	s.l = s.l[1:]
	return b
}

func (s *blockQueue) IsEmpty() bool {
	return len(s.l) == 0
}

func (m *MeshDB) ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, blockHandler func(block *types.Block) error) error {
	blocksToVisit := &blockQueue{}
	for id := range view {
		block, err := m.GetBlock(id)
		if err != nil {
			return err
		}
		blocksToVisit.Push(block)
	}
	seenBlocks := make(map[types.BlockID]struct{})
	for !blocksToVisit.IsEmpty() {
		block := blocksToVisit.Pop()

		//execute handler
		if err := blockHandler(block); err != nil {
			return err
		}

		//push children to bfs queue
		for _, id := range block.ViewEdges {
			if _, found := seenBlocks[id]; found {
				continue
			}
			if bChild, err := m.GetBlock(id); err != nil {
				return err
			} else {
				if bChild.LayerIndex >= layer { //dont traverse too deep
					if _, found := seenBlocks[id]; !found {
						seenBlocks[id] = struct{}{}
						blocksToVisit.Push(bChild)
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
		return nil, fmt.Errorf("error getting layer %v from database %v", index, err)
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
	bytes, err := types.InterfaceToBytes(bl)
	if err != nil {
		return fmt.Errorf("could not encode bl")
	}

	m.blockCache.put(bl) // must be called before the go routine, to allow teardown

	go func() {
		if err := m.blocks.Put(bl.ID().ToBytes(), bytes); err != nil {
			m.Log.With().Error("could not add bl to database, removing from cache", log.BlockId(uint64(bl.ID())), log.Err(err))

			// teardown
			m.blockCache.remove(bl.ID())
			return
		}

		if err := m.updateLayerWithBlock(&bl.MiniBlock); err != nil {
			m.Log.With().Error("could not update block in layer's db, removing block from block db", log.BlockId(uint64(bl.ID())), log.Err(err)) // teardown

			// teardown
			m.blockCache.remove(bl.ID())
			if err := m.blocks.Delete(bl.ID().ToBytes()); err != nil {
				m.Log.With().Error("failed to clean block db", log.BlockId(uint64(bl.ID())), log.Err(err))
				return
			}
		}
	}()

	return nil
}

func (m *MeshDB) updateLayerWithBlock(blk *types.MiniBlock) error {
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

func (m *MeshDB) writeTransactions(txs []*types.AddressableSignedTransaction) ([]types.TransactionId, error) {
	txids := make([]types.TransactionId, 0, len(txs))
	for _, t := range txs {
		bytes, err := types.AddressableTransactionAsBytes(t)
		if err != nil {
			m.Error("could not marshall tx %v to bytes ", err)
			return nil, err
		}

		id := types.GetTransactionId(t.SerializableSignedTransaction)
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
	map[types.TransactionId]*types.AddressableSignedTransaction,
	[]types.TransactionId) {

	var mIds []types.TransactionId
	ts := make(map[types.TransactionId]*types.AddressableSignedTransaction, len(transactions))
	for _, id := range transactions {
		t, err := m.GetTransaction(id)
		if err != nil {
			m.Warning("could not fetch tx, %v %v", hex.EncodeToString(id[:]), err)
			mIds = append(mIds, id)
		} else {
			ts[id] = t
		}
	}
	return ts, mIds
}

func (m *MeshDB) GetTransaction(id types.TransactionId) (*types.AddressableSignedTransaction, error) {
	tBytes, err := m.transactions.Get(id[:])
	if err != nil {
		return nil, fmt.Errorf("could not find transaction in database %v err=%v", hex.EncodeToString(id[:]), err)
	}
	return types.BytesAsAddressableTransaction(tBytes)
}
