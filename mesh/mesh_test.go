package mesh

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ContextualValidityMock struct {
}

func (m *ContextualValidityMock) Has([]byte) (bool, error) {
	return true, nil
}

func (m *ContextualValidityMock) NewBatch() database.Batch {
	panic("implement me")
}

func (m *ContextualValidityMock) Find([]byte) database.Iterator {
	panic("implement me")
}

func (m *ContextualValidityMock) Put([]byte, []byte) error {
	return nil
}

func (m *ContextualValidityMock) Get([]byte) (value []byte, err error) {
	return constTrue, nil
}

func (m *ContextualValidityMock) Delete([]byte) error {
	return nil
}

func (m *ContextualValidityMock) Close() {

}

type MeshValidatorMock struct {
	mdb *DB
}

func (m *MeshValidatorMock) Persist() error {
	return nil
}

func (m *MeshValidatorMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	return layer.Index().Sub(1), layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer().Sub(1), bl.Layer()
}

type MockState struct{}

func (MockState) ValidateAndAddTxToPool(tx *types.Transaction) error {
	panic("implement me")
}

func (MockState) LoadState(types.LayerID) error {
	panic("implement me")
}

func (MockState) GetStateRoot() types.Hash32 {
	return [32]byte{}
}

func (MockState) ValidateNonceAndBalance(*types.Transaction) error {
	panic("implement me")
}

func (MockState) GetLayerApplied(types.TransactionID) *types.LayerID {
	panic("implement me")
}

func (MockState) ApplyTransactions(types.LayerID, []*types.Transaction) (int, error) {
	return 0, nil
}

func (MockState) ApplyRewards(types.LayerID, []types.Address, *big.Int) {
}

func (MockState) AddressExists(types.Address) bool {
	return true
}

func (MockState) GetAllAccounts() (*types.MultipleAccountsState, error) {
	panic("implement me")
}

func (MockState) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	panic("implement me")
}

func (MockState) GetBalance(addr types.Address) uint64 {
	panic("implement me")
}
func (MockState) GetNonce(addr types.Address) uint64 {
	panic("implement me")
}

type MockTxMemPool struct {
	db map[types.TransactionID]*types.Transaction
}

func newMockTxMemPool() *MockTxMemPool {
	return &MockTxMemPool{
		db: make(map[types.TransactionID]*types.Transaction),
	}
}

func (m *MockTxMemPool) Get(ID types.TransactionID) (*types.Transaction, error) {
	return m.db[ID], nil
}

func (m *MockTxMemPool) Put(ID types.TransactionID, t *types.Transaction) {
	m.db[ID] = t
}

func (MockTxMemPool) Invalidate(types.TransactionID) {

}

func getMesh(tb testing.TB, id string) *Mesh {
	lg := logtest.New(tb)
	mmdb := NewMemMeshDB(lg)
	return NewMesh(mmdb, NewAtxDbMock(), ConfigTst(), &MeshValidatorMock{mdb: mmdb}, newMockTxMemPool(), &MockState{}, lg)
}

func addLayer(r *require.Assertions, id types.LayerID, layerSize int, msh *Mesh) *types.Layer {
	for i := 0; i < layerSize; i++ {
		txIDs, _ := addManyTXsToPool(r, msh, 4)
		block := types.NewExistingBlock(id, []byte(rand.String(8)), txIDs)
		block.Initialize()
		err := msh.AddBlockWithTxs(context.TODO(), block)
		r.NoError(err, "cannot add data to test")
		msh.contextualValidity.Put(block.ID().Bytes(), []byte{1})
	}
	l, err := msh.GetLayer(id)
	r.NoError(err, "cant get a layer we've just created")
	return l
}

func TestMesh_AddLayerGetLayer(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "t2")
	defer msh.Close()

	id := types.NewLayerID(1)
	_, err := msh.GetLayer(id)
	r.EqualError(err, database.ErrNotFound.Error())

	txIDs1, _ := addManyTXsToPool(r, msh, 4)
	txIDs2, _ := addManyTXsToPool(r, msh, 3)
	txIDs3, _ := addManyTXsToPool(r, msh, 6)
	r.NoError(msh.AddBlockWithTxs(context.TODO(), types.NewExistingBlock(id, []byte("data1"), txIDs1)))
	r.NoError(msh.AddBlockWithTxs(context.TODO(), types.NewExistingBlock(id, []byte("data2"), txIDs2)))
	r.NoError(msh.AddBlockWithTxs(context.TODO(), types.NewExistingBlock(id, []byte("data3"), txIDs3)))

	lyr, err := msh.GetLayer(id)
	r.NoError(err)
	r.Equal(id, lyr.Index())
	r.Equal(3, len(lyr.Blocks()))
}

func TestMesh_ProcessedLayer(t *testing.T) {
	msh := getMesh(t, "t6")
	defer msh.Close()
	msh.setProcessedLayer(types.NewLayerID(2), types.Hash32{})
	assert.Equal(t, types.NewLayerID(2), msh.ProcessedLayer())
	msh.setProcessedLayer(types.NewLayerID(3), types.Hash32{})
	assert.Equal(t, types.NewLayerID(3), msh.ProcessedLayer())
	msh.setProcessedLayer(types.NewLayerID(5), types.Hash32{})
	assert.Equal(t, types.NewLayerID(3), msh.ProcessedLayer())
	msh.setProcessedLayer(types.NewLayerID(4), types.Hash32{})
	assert.Equal(t, types.NewLayerID(4), msh.ProcessedLayer())
	msh.setProcessedLayer(types.NewLayerID(3), types.Hash32{})
	assert.Equal(t, types.NewLayerID(4), msh.ProcessedLayer())
}

func TestMesh_PersistProcessedLayer(t *testing.T) {
	msh := getMesh(t, "persist_processed_layer")
	defer msh.Close()
	lyr := &ProcessedLayer{
		ID:   types.NewLayerID(3),
		Hash: types.CalcHash32([]byte("layer 3 hash")),
	}
	assert.NoError(t, msh.persistProcessedLayer(lyr))
	rLyr, err := msh.recoverProcessedLayer()
	assert.NoError(t, err)
	assert.Equal(t, lyr, rLyr)
}

func TestMesh_LatestKnownLayer(t *testing.T) {
	msh := getMesh(t, "t6")
	defer msh.Close()
	msh.setLatestLayer(types.NewLayerID(3))
	msh.setLatestLayer(types.NewLayerID(7))
	msh.setLatestLayer(types.NewLayerID(10))
	msh.setLatestLayer(types.NewLayerID(1))
	msh.setLatestLayer(types.NewLayerID(2))
	assert.Equal(t, types.NewLayerID(10), msh.LatestLayer(), "wrong layer")
}

func TestMesh_WakeUp(t *testing.T) {
	msh := getMesh(t, "t1")
	defer msh.Close()

	r := require.New(t)
	txIDs1, _ := addManyTXsToPool(r, msh, 4)
	txIDs2, _ := addManyTXsToPool(r, msh, 3)
	block1 := types.NewExistingBlock(types.NewLayerID(1), []byte("data1"), txIDs1)
	block2 := types.NewExistingBlock(types.NewLayerID(2), []byte("data2"), txIDs2)

	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block1))
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block2))

	rBlock2, err := msh.GetBlock(block2.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs2), len(rBlock2.TxIDs), "block TX size was wrong")
	assert.Equal(t, block2.Data, rBlock2.MiniBlock.Data, "block content was wrong")

	rBlock1, err := msh.GetBlock(block1.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs1), len(rBlock1.TxIDs), "block TX size was wrong")
	assert.Equal(t, block1.Data, rBlock1.MiniBlock.Data, "block content was wrong")

	recoveredMesh := NewMesh(msh.DB, NewAtxDbMock(), ConfigTst(), &MeshValidatorMock{mdb: msh.DB}, newMockTxMemPool(), &MockState{}, logtest.New(t))

	rBlock2, err = recoveredMesh.GetBlock(block2.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs2), len(rBlock2.TxIDs), "block TX size was wrong")
	assert.Equal(t, block2.Data, rBlock2.MiniBlock.Data, "block content was wrong")

	rBlock1, err = recoveredMesh.GetBlock(block1.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs1), len(rBlock1.TxIDs), "block TX size was wrong")
	assert.Equal(t, block1.Data, rBlock1.MiniBlock.Data, "block content was wrong")
}

func TestMesh_OrphanBlocks(t *testing.T) {
	msh := getMesh(t, "t6")
	defer msh.Close()
	r := require.New(t)
	txIDs1, _ := addManyTXsToPool(r, msh, 4)
	txIDs2, _ := addManyTXsToPool(r, msh, 3)
	txIDs3, _ := addManyTXsToPool(r, msh, 6)
	txIDs4, _ := addManyTXsToPool(r, msh, 7)
	txIDs5, _ := addManyTXsToPool(r, msh, 3)
	block1 := types.NewExistingBlock(types.NewLayerID(1), []byte("data data data1"), txIDs1)
	block2 := types.NewExistingBlock(types.NewLayerID(1), []byte("data data data2"), txIDs2)
	block3 := types.NewExistingBlock(types.NewLayerID(2), []byte("data data data3"), txIDs3)
	block4 := types.NewExistingBlock(types.NewLayerID(2), []byte("data data data4"), txIDs4)
	block5 := types.NewExistingBlock(types.NewLayerID(3), []byte("data data data5"), txIDs5)
	block5.ForDiff = append(block5.ForDiff, block1.ID())
	block5.ForDiff = append(block5.ForDiff, block2.ID())
	block5.ForDiff = append(block5.ForDiff, block3.ID())
	block5.ForDiff = append(block5.ForDiff, block4.ID())
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block1))
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block2))
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block3))
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block4))
	arr, _ := msh.GetOrphanBlocksBefore(types.NewLayerID(3))
	assert.Equal(t, 4, len(arr), "wrong number of orphaned blocks")
	arr2, _ := msh.GetOrphanBlocksBefore(types.NewLayerID(2))
	assert.Equal(t, 2, len(arr2), "wrong number of orphaned blocks")
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block5))
	time.Sleep(1 * time.Second)
	arr3, _ := msh.GetOrphanBlocksBefore(types.NewLayerID(4))
	assert.Equal(t, 1, len(arr3), "wrong number of orphaned blocks")
}

func TestMesh_OrphanBlocksClearEmptyLayers(t *testing.T) {
	msh := getMesh(t, "t6")
	defer msh.Close()
	r := require.New(t)
	txIDs1, _ := addManyTXsToPool(r, msh, 4)
	txIDs2, _ := addManyTXsToPool(r, msh, 3)
	txIDs3, _ := addManyTXsToPool(r, msh, 6)
	txIDs4, _ := addManyTXsToPool(r, msh, 7)
	txIDs5, _ := addManyTXsToPool(r, msh, 3)
	block1 := types.NewExistingBlock(types.NewLayerID(1), []byte("data data data1"), txIDs1)
	block2 := types.NewExistingBlock(types.NewLayerID(1), []byte("data data data2"), txIDs2)
	block3 := types.NewExistingBlock(types.NewLayerID(2), []byte("data data data3"), txIDs3)
	block4 := types.NewExistingBlock(types.NewLayerID(2), []byte("data data data4"), txIDs4)
	block5 := types.NewExistingBlock(types.NewLayerID(3), []byte("data data data5"), txIDs5)
	block5.ForDiff = append(block5.ForDiff, block1.ID())
	block5.ForDiff = append(block5.ForDiff, block2.ID())
	block5.ForDiff = append(block5.ForDiff, block3.ID())
	block5.ForDiff = append(block5.ForDiff, block4.ID())
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block1))
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block2))
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block3))
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block4))
	arr, _ := msh.GetOrphanBlocksBefore(types.NewLayerID(3))
	assert.Equal(t, 4, len(arr), "wrong number of orphaned blocks")
	arr2, _ := msh.GetOrphanBlocksBefore(types.NewLayerID(2))
	assert.Equal(t, 2, len(arr2), "wrong number of orphaned blocks")
	assert.NoError(t, msh.AddBlockWithTxs(context.TODO(), block5))
	assert.Equal(t, 1, len(msh.orphanBlocks))
}

func TestMesh_AddBlockWithTxs_PushTransactions_UpdateUnappliedTxs(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "mesh")

	state := &MockMapState{}
	msh.txProcessor = state

	layerID := types.GetEffectiveGenesis().Add(1)
	signer, origin := newSignerAndAddress(r, "origin")
	tx1 := addTxToMesh(r, msh, signer, 2468)
	tx2 := addTxToMesh(r, msh, signer, 2469)
	tx3 := addTxToMesh(r, msh, signer, 2470)
	tx4 := addTxToMesh(r, msh, signer, 2471)
	tx5 := addTxToMesh(r, msh, signer, 2472)
	addBlockWithTxs(r, msh, layerID, true, tx1, tx2)
	addBlockWithTxs(r, msh, layerID, true, tx2, tx3, tx4)
	addBlockWithTxs(r, msh, layerID, false, tx4, tx5)
	addBlockWithTxs(r, msh, layerID, false, tx5)

	txns := getTxns(r, msh.DB, origin)
	r.Len(txns, 5)
	for i := 0; i < 5; i++ {
		r.Equal(2468+i, int(txns[i].Nonce))
		r.Equal(111, int(txns[i].TotalAmount))
	}

	msh.pushLayersToState(types.GetEffectiveGenesis().Add(1), types.GetEffectiveGenesis().Add(2))
	r.Equal(4, len(state.Txs))

	r.ElementsMatch(GetTransactionIds(tx5), GetTransactionIds(state.Pool...))

	txns = getTxns(r, msh.DB, origin)
	r.Empty(txns)
}

func TestMesh_AddBlockWithTxs_PushTransactions_getInvalidBlocksByHare(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "mesh")

	state := &MockMapState{}
	msh.txProcessor = state

	layerID := types.NewLayerID(1)
	signer, _ := newSignerAndAddress(r, "origin")
	tx1 := addTxToMesh(r, msh, signer, 2468)
	tx2 := addTxToMesh(r, msh, signer, 2469)
	tx3 := addTxToMesh(r, msh, signer, 2470)
	tx4 := addTxToMesh(r, msh, signer, 2471)
	tx5 := addTxToMesh(r, msh, signer, 2472)
	var blocks []*types.Block
	blocks = append(blocks, addBlockWithTxs(r, msh, layerID, true, tx1, tx2))
	blocks = append(blocks, addBlockWithTxs(r, msh, layerID, true, tx2, tx3, tx4))
	blocks = append(blocks, addBlockWithTxs(r, msh, layerID, true, tx4, tx5))
	hareBlocks := blocks[:2]
	invalid := msh.getInvalidBlocksByHare(context.TODO(), types.NewExistingLayer(layerID, hareBlocks))
	r.ElementsMatch(blocks[2:], invalid)

	msh.reInsertTxsToPool(hareBlocks, invalid, layerID)
	r.ElementsMatch(GetTransactionIds(tx5), GetTransactionIds(state.Pool...))
}

func TestMesh_ExtractUniqueOrderedTransactions(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "t2")
	defer msh.Close()
	layerID := types.NewLayerID(1)
	signer, _ := newSignerAndAddress(r, "origin")
	tx1 := addTxToMesh(r, msh, signer, 2468)
	tx2 := addTxToMesh(r, msh, signer, 2469)
	tx3 := addTxToMesh(r, msh, signer, 2470)
	tx4 := addTxToMesh(r, msh, signer, 2471)
	addBlockWithTxs(r, msh, layerID, true, tx1, tx2)
	addBlockWithTxs(r, msh, layerID, true, tx2, tx3, tx4)
	addBlockWithTxs(r, msh, layerID, false, tx4)
	l, err := msh.GetLayer(layerID)
	r.NoError(err)

	validBlocks := msh.extractUniqueOrderedTransactions(l)

	r.ElementsMatch(GetTransactionIds(tx1, tx2, tx3, tx4), GetTransactionIds(validBlocks...))
}

func TestMesh_persistLayerHashes(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "persistLayerHashes")
	defer msh.Close()

	// test first layer hash
	l := addLayer(r, types.GetEffectiveGenesis(), 5, msh)
	msh.persistLayerHashes(l)
	wantedHash := types.CalcAggregateHash32(types.Hash32{}, l.Hash().Bytes())
	actualHash, err := msh.getAggregatedLayerHash(types.GetEffectiveGenesis())
	assert.NoError(t, err)

	assert.Equal(t, wantedHash, actualHash)

	l2 := addLayer(r, types.GetEffectiveGenesis().Add(1), 5, msh)
	msh.persistLayerHashes(l2)
	secondWantedHash := types.CalcAggregateHash32(wantedHash, l2.Hash().Bytes())
	actualHash2, err := msh.getAggregatedLayerHash(types.GetEffectiveGenesis().Add(1))
	assert.NoError(t, err)
	assert.Equal(t, secondWantedHash, actualHash2)
}

func GetTransactionIds(txs ...*types.Transaction) []types.TransactionID {
	var res []types.TransactionID
	for _, tx := range txs {
		res = append(res, tx.ID())
	}
	return res
}

func addTxToMesh(r *require.Assertions, msh *Mesh, signer *signing.EdSigner, nonce uint64) *types.Transaction {
	tx1 := newTx(r, signer, nonce, 111)
	blk := &types.Block{}
	blk.LayerIndex = types.NewLayerID(1)
	err := msh.writeTransactions(blk, tx1)
	r.NoError(err)
	return tx1
}

func addBlockWithTxs(r *require.Assertions, msh *Mesh, id types.LayerID, valid bool, txs ...*types.Transaction) *types.Block {
	blk := types.NewExistingBlock(id, []byte("data"), nil)
	for _, tx := range txs {
		blk.TxIDs = append(blk.TxIDs, tx.ID())
		msh.txPool.Put(tx.ID(), tx)
	}
	blk.Initialize()
	err := msh.SaveContextualValidity(blk.ID(), valid)
	r.NoError(err)

	err = msh.AddBlockWithTxs(context.TODO(), blk)
	r.NoError(err)
	return blk
}

func addManyTXsToPool(r *require.Assertions, msh *Mesh, numOfTxs int) ([]types.TransactionID, []*types.Transaction) {
	txs := make([]*types.Transaction, numOfTxs)
	txIDs := make([]types.TransactionID, numOfTxs)
	for i := 0; i < numOfTxs; i++ {
		tx, err := types.NewSignedTx(1, types.HexToAddress("1"), 10, 100, rand.Uint64(), signing.NewEdSigner())
		r.NoError(err)
		txs[i] = tx
		txIDs[i] = tx.ID()
		msh.txPool.Put(tx.ID(), tx)
	}
	return txIDs, txs
}

func TestMesh_AddBlockWithTxs(t *testing.T) {
	r := require.New(t)
	mesh := getMesh(t, "AddBlockWithTxs")

	numTXs := 6
	txIDs, _ := addManyTXsToPool(r, mesh, numTXs)

	block := types.NewExistingBlock(types.NewLayerID(1), []byte("data"), txIDs)
	r.NoError(mesh.AddBlockWithTxs(context.TODO(), block))

	res, err := mesh.GetBlock(block.ID())
	assert.NoError(t, err)
	assert.Equal(t, numTXs, len(res.TxIDs), "block TX size was wrong")
	assert.Equal(t, block.Data, res.MiniBlock.Data, "block content was wrong")
}
