package mesh

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/big"
	"testing"
	"time"
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
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block) (types.LayerID, types.LayerID) {
	return bl.Layer() - 1, bl.Layer()
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

func (MockState) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	panic("implement me")
}

func (MockState) GetBalance(addr types.Address) uint64 {
	panic("implement me")
}
func (MockState) GetNonce(addr types.Address) uint64 {
	panic("implement me")
}

type MockTxMemPool struct{}

func (MockTxMemPool) Get(types.TransactionID) (*types.Transaction, error) {
	return &types.Transaction{}, nil
}

func (MockTxMemPool) Put(types.TransactionID, *types.Transaction) {

}

func (MockTxMemPool) Invalidate(types.TransactionID) {

}

type MockAtxMemPool struct{}

func (MockAtxMemPool) Get(types.ATXID) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (MockAtxMemPool) Put(*types.ActivationTx) {

}

func (MockAtxMemPool) Invalidate(types.ATXID) {

}

func getMesh(id string) *Mesh {
	lg := log.New(id, "", "")
	mmdb := NewMemMeshDB(lg)
	layers := NewMesh(mmdb, NewAtxDbMock(), ConfigTst(), &MeshValidatorMock{mdb: mmdb}, MockTxMemPool{}, &MockState{}, lg)
	return layers
}

func TestLayers_AddBlock(t *testing.T) {

	layers := getMesh("t1")
	defer layers.Close()

	block1 := types.NewExistingBlock(1, []byte("data1"))
	block2 := types.NewExistingBlock(2, []byte("data2"))
	block3 := types.NewExistingBlock(3, []byte("data3"))

	addTransactionsWithFee(t, layers.DB, block1, 4, rand.Int63n(100))

	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)

	rBlock2, err := layers.GetBlock(block2.ID())
	assert.NoError(t, err)

	rBlock1, err := layers.GetBlock(block1.ID())
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.TxIDs) == len(block1.TxIDs), "block content was wrong")
	assert.True(t, bytes.Compare(rBlock2.MiniBlock.Data, []byte("data2")) == 0, "block content was wrong")
	//assert.True(t, len(*rBlock1.ActiveSet) == len(*block1.ActiveSet))
}

func TestLayers_AddLayer(t *testing.T) {
	r := require.New(t)

	msh := getMesh("t2")
	defer msh.Close()

	id := types.LayerID(1)

	_, err := msh.GetLayer(id)
	r.EqualError(err, database.ErrNotFound.Error())

	err = msh.AddBlock(types.NewExistingBlock(id, []byte("data1")))
	r.NoError(err)
	err = msh.AddBlock(types.NewExistingBlock(id, []byte("data2")))
	r.NoError(err)
	err = msh.AddBlock(types.NewExistingBlock(id, []byte("data3")))
	r.NoError(err)
	_, err = msh.GetLayer(id)
	r.NoError(err)
}

func TestLayers_AddWrongLayer(t *testing.T) {
	layers := getMesh("t3")
	defer layers.Close()
	block1 := types.NewExistingBlock(1, []byte("data data data1"))
	block2 := types.NewExistingBlock(2, []byte("data data data2"))
	block3 := types.NewExistingBlock(4, []byte("data data data3"))
	l1 := types.NewExistingLayer(1, []*types.Block{block1})
	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.SaveContextualValidity(block1.ID(), true)
	assert.NoError(t, err)
	layers.ValidateLayer(l1)
	l2 := types.NewExistingLayer(2, []*types.Block{block2})
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	layers.ValidateLayer(l2)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)
	_, err = layers.GetProcessedLayer(1)
	assert.NoError(t, err)
	_, err = layers.GetProcessedLayer(2)
	assert.NoError(t, err)
	_, err = layers.GetProcessedLayer(4)
	assert.EqualError(t, err, "layer not verified yet")
}

func TestLayers_GetLayer(t *testing.T) {
	layers := getMesh("t4")
	defer layers.Close()
	block1 := types.NewExistingBlock(1, []byte("data data data1"))
	block2 := types.NewExistingBlock(1, []byte("data data data2"))
	block3 := types.NewExistingBlock(1, []byte("data data data3"))
	l1 := types.NewExistingLayer(1, []*types.Block{block1})
	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	layers.ValidateLayer(l1)
	l, err := layers.GetProcessedLayer(0)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)
	l, err = layers.GetProcessedLayer(1)
	assert.True(t, err == nil, "error: ", err)
	assert.True(t, l.Index() == 1, "wrong layer")
}

func TestLayers_LatestKnownLayer(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	layers.SetLatestLayer(3)
	layers.SetLatestLayer(7)
	layers.SetLatestLayer(10)
	layers.SetLatestLayer(1)
	layers.SetLatestLayer(2)
	assert.True(t, layers.LatestLayer() == 10, "wrong layer")
}

func TestLayers_WakeUp(t *testing.T) {
	layers := getMesh("t1")
	defer layers.Close()

	block1 := types.NewExistingBlock(1, []byte("data1"))
	block2 := types.NewExistingBlock(2, []byte("data2"))
	block3 := types.NewExistingBlock(3, []byte("data3"))

	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)

	rBlock2, err := layers.GetBlock(block2.ID())
	assert.NoError(t, err)

	rBlock1, err := layers.GetBlock(block1.ID())
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.TxIDs) == len(block1.TxIDs), "block content was wrong")
	assert.True(t, bytes.Compare(rBlock2.MiniBlock.Data, []byte("data2")) == 0, "block content was wrong")
	//assert.True(t, len(*rBlock1.ActiveSet) == len(*block1.ActiveSet))

	recoveredMesh := NewMesh(layers.DB, NewAtxDbMock(), ConfigTst(), &MeshValidatorMock{mdb: layers.DB}, MockTxMemPool{}, &MockState{}, log.New("", "", ""))

	rBlock2, err = recoveredMesh.GetBlock(block2.ID())
	assert.NoError(t, err)

	rBlock1, err = recoveredMesh.GetBlock(block1.ID())
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.TxIDs) == len(block1.TxIDs), "block content was wrong")
	assert.True(t, bytes.Compare(rBlock2.MiniBlock.Data, []byte("data2")) == 0, "block content was wrong")
	//assert.True(t, len(rBlock1.ATXIDs) == len(block1.ATXIDs))

}

func TestLayers_OrphanBlocks(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	block1 := types.NewExistingBlock(1, []byte("data data data1"))
	block2 := types.NewExistingBlock(1, []byte("data data data2"))
	block3 := types.NewExistingBlock(2, []byte("data data data3"))
	block4 := types.NewExistingBlock(2, []byte("data data data4"))
	block5 := types.NewExistingBlock(3, []byte("data data data5"))
	block5.AddView(block1.ID())
	block5.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)
	err = layers.AddBlock(block4)
	assert.NoError(t, err)
	arr, _ := layers.GetOrphanBlocksBefore(3)
	assert.True(t, len(arr) == 4, "wrong layer")
	arr2, _ := layers.GetOrphanBlocksBefore(2)
	assert.Equal(t, len(arr2), 2)
	err = layers.AddBlock(block5)
	assert.NoError(t, err)
	time.Sleep(1 * time.Second)
	arr3, _ := layers.GetOrphanBlocksBefore(4)
	assert.True(t, len(arr3) == 1, "wrong layer")

}

func TestLayers_OrphanBlocksClearEmptyLayers(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	block1 := types.NewExistingBlock(1, []byte("data data data1"))
	block2 := types.NewExistingBlock(1, []byte("data data data2"))
	block3 := types.NewExistingBlock(2, []byte("data data data3"))
	block4 := types.NewExistingBlock(2, []byte("data data data4"))
	block5 := types.NewExistingBlock(3, []byte("data data data5"))
	block5.AddView(block1.ID())
	block5.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)
	err = layers.AddBlock(block4)
	assert.NoError(t, err)
	arr, _ := layers.GetOrphanBlocksBefore(3)
	assert.True(t, len(arr) == 4, "wrong layer")
	arr2, _ := layers.GetOrphanBlocksBefore(2)
	assert.Equal(t, len(arr2), 2)
	err = layers.AddBlock(block5)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(layers.orphanBlocks))
}

type MockBlockBuilder struct {
	txs []*types.Transaction
}

func (m *MockBlockBuilder) ValidateAndAddTxToPool(tx *types.Transaction) error {
	m.txs = append(m.txs, tx)
	return nil
}

func TestMesh_AddBlockWithTxs_PushTransactions_UpdateUnappliedTxs(t *testing.T) {
	r := require.New(t)

	msh := getMesh("mesh")

	state := &MockMapState{}
	msh.txProcessor = state

	layerID := types.LayerID(types.GetEffectiveGenesis() + 1)
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

	msh.pushLayersToState(types.GetEffectiveGenesis()+1, types.GetEffectiveGenesis()+2)
	r.Equal(4, len(state.Txs))

	r.ElementsMatch(GetTransactionIds(tx5), GetTransactionIds(state.Pool...))

	txns = getTxns(r, msh.DB, origin)
	r.Empty(txns)
}

func TestMesh_AddBlockWithTxs_PushTransactions_getInvalidBlocksByHare(t *testing.T) {
	r := require.New(t)

	msh := getMesh("mesh")

	state := &MockMapState{}
	msh.txProcessor = state

	layerID := types.LayerID(1)
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
	invalid := msh.getInvalidBlocksByHare(types.NewExistingLayer(layerID, hareBlocks))
	r.ElementsMatch(blocks[2:], invalid)

	msh.reInsertTxsToPool(hareBlocks, invalid, layerID)
	r.ElementsMatch(GetTransactionIds(tx5), GetTransactionIds(state.Pool...))

}

func TestMesh_ExtractUniqueOrderedTransactions(t *testing.T) {
	r := require.New(t)

	msh := getMesh("t2")
	defer msh.Close()
	layerID := types.LayerID(1)
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

func GetTransactionIds(txs ...*types.Transaction) []types.TransactionID {
	var res []types.TransactionID
	for _, tx := range txs {
		res = append(res, tx.ID())
	}
	return res
}

func addTxToMesh(r *require.Assertions, msh *Mesh, signer *signing.EdSigner, nonce uint64) *types.Transaction {
	tx1 := newTx(r, signer, nonce, 111)
	err := msh.writeTransactions(0, []*types.Transaction{tx1})
	r.NoError(err)
	return tx1
}

func addBlockWithTxs(r *require.Assertions, msh *Mesh, id types.LayerID, valid bool, txs ...*types.Transaction) *types.Block {
	blk := types.NewExistingBlock(id, []byte("data"))
	for _, tx := range txs {
		blk.TxIDs = append(blk.TxIDs, tx.ID())
	}
	blk.Initialize()
	err := msh.SaveContextualValidity(blk.ID(), valid)
	r.NoError(err)
	err = msh.AddBlockWithTxs(blk, txs, nil)
	r.NoError(err)
	return blk
}

type FailingAtxDbMock struct{}

func (FailingAtxDbMock) ProcessAtxs([]*types.ActivationTx) error { return fmt.Errorf("💥") }

func (FailingAtxDbMock) GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error) {
	panic("implement me")
}

func (FailingAtxDbMock) GetFullAtx(types.ATXID) (*types.ActivationTx, error) { panic("implement me") }

func (FailingAtxDbMock) SyntacticallyValidateAtx(*types.ActivationTx) error { panic("implement me") }

func TestMesh_AddBlockWithTxs(t *testing.T) {
	r := require.New(t)
	lg := log.New("id", "", "")
	meshDB := NewMemMeshDB(lg)
	mesh := NewMesh(meshDB, &FailingAtxDbMock{}, ConfigTst(), &MeshValidatorMock{mdb: meshDB}, MockTxMemPool{}, &MockState{}, lg)

	blk := types.NewExistingBlock(1, []byte("data"))

	err := mesh.AddBlockWithTxs(blk, nil, nil)
	r.EqualError(err, "failed to process ATXs: 💥")
	_, err = meshDB.blocks.Get(blk.ID().Bytes())
	r.EqualError(err, "leveldb: not found")
}
