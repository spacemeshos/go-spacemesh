package mesh

import (
	"bytes"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

func (m *ContextualValidityMock) Put(key, value []byte) error {
	return nil
}

func (m *ContextualValidityMock) Get(key []byte) (value []byte, err error) {
	return TRUE, nil
}

func (m *ContextualValidityMock) Delete(key []byte) error {
	return nil
}

func (m *ContextualValidityMock) Close() {

}

type MeshValidatorMock struct {
	mdb *MeshDB
}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *types.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id types.LayerID)) {}

type MockState struct{}

func (MockState) ValidateSignature(signed types.Signed) (types.Address, error) {
	return types.Address{}, nil
}

func (MockState) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (uint32, error) {
	return 0, nil
}

func (MockState) ApplyRewards(layer types.LayerID, miners []types.Address, underQuota map[types.Address]int, bonusReward, diminishedReward *big.Int) {
}

func (MockState) AddressExists(addr types.Address) bool {
	return true
}

type MockTxMemPool struct{}

func (MockTxMemPool) Get(id types.TransactionId) (types.Transaction, error) {
	return types.Transaction{}, nil
}
func (MockTxMemPool) GetAllItems() []*types.Transaction {
	return nil
}
func (MockTxMemPool) Put(id types.TransactionId, item *types.Transaction) {

}
func (MockTxMemPool) Invalidate(id types.TransactionId) {

}

type MockAtxMemPool struct{}

func (MockAtxMemPool) Get(id types.AtxId) (types.ActivationTx, error) {
	return types.ActivationTx{}, nil
}

func (MockAtxMemPool) GetAllItems() []types.ActivationTx {
	return nil
}

func (MockAtxMemPool) Put(id types.AtxId, item *types.ActivationTx) {

}

func (MockAtxMemPool) Invalidate(id types.AtxId) {

}

func getMesh(id string) *Mesh {
	lg := log.New(id, "", "")
	mmdb := NewMemMeshDB(lg)
	layers := NewMesh(mmdb, &AtxDbMock{}, ConfigTst(), &MeshValidatorMock{mdb: mmdb}, MockTxMemPool{}, MockAtxMemPool{}, &MockState{}, lg)
	return layers
}

func TestLayers_AddBlock(t *testing.T) {

	layers := getMesh("t1")
	defer layers.Close()

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))
	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2, []byte("data2"))
	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 3, []byte("data3"))

	addTransactionsWithGas(layers.MeshDB, block1, 4, rand.Int63n(100))

	err := layers.AddBlock(block1)
	assert.NoError(t, err)
	err = layers.AddBlock(block2)
	assert.NoError(t, err)
	err = layers.AddBlock(block3)
	assert.NoError(t, err)

	rBlock2, err := layers.GetBlock(block2.Id)
	assert.NoError(t, err)

	rBlock1, err := layers.GetBlock(block1.Id)
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.TxIds) == len(block1.TxIds), "block content was wrong")
	assert.True(t, bytes.Compare(rBlock2.MiniBlock.Data, []byte("data2")) == 0, "block content was wrong")
	assert.True(t, len(rBlock1.AtxIds) == len(block1.AtxIds))
}

func TestLayers_AddLayer(t *testing.T) {
	r := require.New(t)

	msh := getMesh("t2")
	defer msh.Close()

	id := types.LayerID(1)

	_, err := msh.GetLayer(id)
	r.EqualError(err, "error getting layer 1 from database leveldb: not found")

	err = msh.AddBlock(types.NewExistingBlock(types.BlockID(uuid.New().ID()), id, []byte("data")))
	r.NoError(err)
	err = msh.AddBlock(types.NewExistingBlock(types.BlockID(uuid.New().ID()), id, []byte("data")))
	r.NoError(err)
	err = msh.AddBlock(types.NewExistingBlock(types.BlockID(uuid.New().ID()), id, []byte("data")))
	r.NoError(err)
	l, err := msh.GetLayer(id)
	r.NoError(err)
	r.True(string(l.Blocks()[1].MiniBlock.Data) == "data", "wrong block data ")
}

func TestLayers_AddWrongLayer(t *testing.T) {
	layers := getMesh("t3")
	defer layers.Close()
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 4, []byte("data data data"))
	l1 := types.NewExistingLayer(1, []*types.Block{block1})
	layers.AddBlock(block1)
	layers.ValidateLayer(l1)
	l2 := types.NewExistingLayer(2, []*types.Block{block2})
	layers.AddBlock(block2)
	layers.ValidateLayer(l2)
	layers.AddBlock(block3)
	_, err := layers.GetVerifiedLayer(1)
	assert.True(t, err == nil, "error: ", err)
	_, err1 := layers.GetVerifiedLayer(2)
	assert.True(t, err1 == nil, "error: ", err1)
	_, err2 := layers.GetVerifiedLayer(4)
	assert.True(t, err2 != nil, "added wrong layer ", err2)
}

func TestLayers_GetLayer(t *testing.T) {
	layers := getMesh("t4")
	defer layers.Close()
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	l1 := types.NewExistingLayer(1, []*types.Block{block1})
	layers.AddBlock(block1)
	layers.ValidateLayer(l1)
	l, err := layers.GetVerifiedLayer(0)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	l, err = layers.GetVerifiedLayer(1)
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
	//layers := getMesh(make(chan Peer),  "t5")
	//defer layers.Close()
	//layers.SetLatestLayer(10)
	//assert.True(t, layers.LocalLayerCount() == 10, "wrong layer")
}

func TestLayers_OrphanBlocks(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 3, []byte("data data data"))
	block5.AddView(block1.ID())
	block5.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)
	arr, _ := layers.GetOrphanBlocksBefore(3)
	assert.True(t, len(arr) == 4, "wrong layer")
	arr2, _ := layers.GetOrphanBlocksBefore(2)
	assert.Equal(t, len(arr2), 2)
	layers.AddBlock(block5)
	time.Sleep(1 * time.Second)
	arr3, _ := layers.GetOrphanBlocksBefore(4)
	assert.True(t, len(arr3) == 1, "wrong layer")

}

func TestLayers_OrphanBlocksClearEmptyLayers(t *testing.T) {
	layers := getMesh("t6")
	defer layers.Close()
	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block2 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data data data"))
	block3 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block4 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 2, []byte("data data data"))
	block5 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 3, []byte("data data data"))
	block5.AddView(block1.ID())
	block5.AddView(block2.ID())
	block5.AddView(block3.ID())
	block5.AddView(block4.ID())
	layers.AddBlock(block1)
	layers.AddBlock(block2)
	layers.AddBlock(block3)
	layers.AddBlock(block4)
	arr, _ := layers.GetOrphanBlocksBefore(3)
	assert.True(t, len(arr) == 4, "wrong layer")
	arr2, _ := layers.GetOrphanBlocksBefore(2)
	assert.Equal(t, len(arr2), 2)
	layers.AddBlock(block5)
	assert.Equal(t, 1, len(layers.orphanBlocks))
}

type MockBlockBuilder struct {
	txs []*types.Transaction
}

func (m *MockBlockBuilder) ValidateAndAddTxToPool(tx *types.Transaction, postValidationFunc func()) error {
	m.txs = append(m.txs, tx)
	return nil
}

func TestMesh_AddBlockWithTxs_PushTransactions_UpdateMeshTxs(t *testing.T) {
	r := require.New(t)

	msh := getMesh("mesh")
	blockBuilder := &MockBlockBuilder{}
	msh.SetBlockBuilder(blockBuilder)

	layerID := types.LayerID(1)
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

	txns := getTxns(r, msh.MeshDB, origin)
	r.Len(txns, 5)
	for i := 0; i < 5; i++ {
		r.Equal(2468+i, int(txns[i].Nonce))
		r.Equal(111, int(txns[i].TotalAmount))
	}

	msh.PushTransactions(1, 2)
	r.ElementsMatch(GetTransactionIds(tx4, tx5), GetTransactionIds(blockBuilder.txs...))

	txns = getTxns(r, msh.MeshDB, origin)
	r.Empty(txns)
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
	tx5 := addTxToMesh(r, msh, signer, 2472)
	addBlockWithTxs(r, msh, layerID, true, tx1, tx2)
	addBlockWithTxs(r, msh, layerID, true, tx2, tx3, tx4)
	addBlockWithTxs(r, msh, layerID, false, tx4, tx5)
	addBlockWithTxs(r, msh, layerID, false, tx5)
	l, err := msh.GetLayer(layerID)
	r.NoError(err)

	validBlocks, invalidBlocks := msh.ExtractUniqueTransactions(l)

	r.ElementsMatch(GetTransactionIds(tx1, tx2, tx3, tx4), GetTransactionIds(validBlocks...))
	r.ElementsMatch(GetTransactionIds(tx4, tx5), GetTransactionIds(invalidBlocks...))
}

func GetTransactionIds(txs ...*types.Transaction) []types.TransactionId {
	var res []types.TransactionId
	for _, tx := range txs {
		res = append(res, tx.Id())
	}
	return res
}

func addTxToMesh(r *require.Assertions, msh *Mesh, signer *signing.EdSigner, nonce uint64) *types.Transaction {
	tx1 := newTx(r, signer, nonce, 111)
	err := msh.writeTransactions([]*types.Transaction{tx1})
	r.NoError(err)
	return tx1
}

func addBlockWithTxs(r *require.Assertions, msh *Mesh, id types.LayerID, valid bool, txs ...*types.Transaction) *types.Block {
	blk := types.NewExistingBlock(types.BlockID(uuid.New().ID()), id, []byte("data"))
	for _, tx := range txs {
		blk.TxIds = append(blk.TxIds, tx.Id())
	}
	err := msh.SaveContextualValidity(blk.Id, valid)
	r.NoError(err)
	err = msh.AddBlockWithTxs(blk, txs, nil)
	r.NoError(err)
	return blk
}
