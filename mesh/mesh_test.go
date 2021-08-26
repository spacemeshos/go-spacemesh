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

func (m *MeshValidatorMock) Persist(context.Context) error {
	return nil
}

func (m *MeshValidatorMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *MeshValidatorMock) HandleIncomingLayer(_ context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	return layerID.Sub(1), layerID, false
}

func (m *MeshValidatorMock) HandleLateBlocks(_ context.Context, bl []*types.Block) (types.LayerID, types.LayerID) {
	return bl[0].Layer().Sub(1), bl[0].Layer()
}

type MockState struct{}

func (MockState) ValidateAndAddTxToPool(*types.Transaction) error {
	return nil
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

func (MockState) GetLayerStateRoot(types.LayerID) (types.Hash32, error) {
	panic("implement me")
}

func (MockState) GetBalance(types.Address) uint64 {
	panic("implement me")
}

func (MockState) GetNonce(types.Address) uint64 {
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
	lg := logtest.New(tb).WithName(id)
	mmdb := NewMemMeshDB(lg)
	return NewMesh(mmdb, NewAtxDbMock(), ConfigTst(), &MeshValidatorMock{mdb: mmdb}, newMockTxMemPool(), &MockState{}, lg)
}

func addLayer(r *require.Assertions, id types.LayerID, layerSize int, msh *Mesh) *types.Layer {
	if layerSize == 0 {
		r.NoError(msh.SetZeroBlockLayer(id))
	} else {
		for i := 0; i < layerSize; i++ {
			txIDs, _ := addManyTXsToPool(r, msh, 4)
			block := types.NewExistingBlock(id, []byte(rand.String(8)), txIDs)
			block.Initialize()
			err := msh.AddBlockWithTxs(context.TODO(), block)
			r.NoError(err, "cannot add data to test")
			r.NoError(msh.contextualValidity.Put(block.ID().Bytes(), []byte{1}))
		}
	}
	l, err := msh.GetLayer(id)
	r.NoError(err, "cant get a layer we've just created")
	return l
}

func TestMesh_GetLayerHash(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "get layer hash")
	t.Cleanup(func() {
		msh.Close()
	})

	lyrID := types.NewLayerID(1)
	lyr, err := msh.GetLayer(lyrID)
	r.Equal(database.ErrNotFound, err)
	r.Nil(lyr)

	numBlocks := 10
	lyr = addLayer(r, lyrID, numBlocks, msh)
	r.NoError(msh.SaveContextualValidity(lyr.Blocks()[0].ID(), lyrID, false))

	// before a layer is validated, all blocks count towards its hash, valid or not
	lyr, err = msh.GetLayer(lyrID)
	r.NoError(err)
	assert.Equal(t, numBlocks, len(lyr.Blocks()))
	assert.Equal(t, lyr.Hash(), msh.GetLayerHash(lyrID))

	validBlocks := lyr.Blocks()[1:]
	msh.ValidateLayer(context.TODO(), lyr)
	// after a layer is validated, only contextually valid blocks count towards its hash
	expHash := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(validBlocks)), nil)
	assert.Equal(t, expHash, msh.GetLayerHash(lyrID))
	assert.NotEqual(t, expHash, lyr.Hash())

	// if a late block is received and is considered invalid. hash does not change
	txIDs, _ := addManyTXsToPool(r, msh, 10)
	block := types.NewExistingBlock(lyrID, []byte(rand.String(8)), txIDs)
	block.Initialize()
	err = msh.AddBlockWithTxs(context.TODO(), block)
	r.NoError(err)
	lyr, err = msh.GetLayer(lyrID)
	r.NoError(err)
	assert.Equal(t, numBlocks+1, len(lyr.Blocks()))
	msh.ValidateLayer(context.TODO(), lyr)
	assert.Equal(t, expHash, msh.GetLayerHash(lyrID))

	// now if the late block is determined valid, the hash should be updated
	r.NoError(msh.SaveContextualValidity(block.ID(), lyrID, true))
	msh.ValidateLayer(context.TODO(), lyr)
	newExpectedHash := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(append(validBlocks, block))), nil)
	assert.Equal(t, newExpectedHash, msh.GetLayerHash(lyrID))
}

func TestMesh_SetZeroBlockLayer(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "zero block layer")
	t.Cleanup(func() {
		msh.Close()
	})

	lyrID := types.NewLayerID(1)
	lyr, err := msh.GetLayer(lyrID)
	r.Equal(database.ErrNotFound, err)
	r.Nil(lyr)
	err = msh.SetZeroBlockLayer(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, EmptyLayerHash, msh.GetLayerHash(lyrID))

	// it's ok to add to an empty layer
	lyr = addLayer(r, lyrID, 10, msh)
	msh.ValidateLayer(context.TODO(), lyr)
	lyr, err = msh.GetLayer(lyrID)
	r.NoError(err)
	assert.Equal(t, 10, len(lyr.Blocks()))

	// but not okay to set a non-empty layer to an empty layer
	err = msh.SetZeroBlockLayer(lyrID)
	assert.Equal(t, errLayerHasBlock, err)
}

func TestMesh_AddLayerGetLayer(t *testing.T) {
	r := require.New(t)

	msh := getMesh(t, "t2")
	t.Cleanup(func() {
		msh.Close()
	})

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

func TestMesh_GetAggregatedLayerHash(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "aggregated hash")
	t.Cleanup(func() {
		msh.Close()
	})

	gLyr := types.GetEffectiveGenesis()
	prevHash := EmptyLayerHash
	for i := types.NewLayerID(1); i.Before(gLyr); i = i.Add(1) {
		lyr := addLayer(r, i, 0, msh)
		msh.ValidateLayer(context.TODO(), lyr)
		aggHash := types.CalcBlocksHash32([]types.BlockID{}, prevHash.Bytes())
		assert.Equal(t, aggHash, msh.GetAggregatedLayerHash(lyr.Index()))
		prevHash = aggHash
	}
	lyr, err := msh.GetLayer(gLyr)
	r.NoError(err)
	assert.Equal(t, 1, len(lyr.Blocks()))
	msh.ValidateLayer(context.TODO(), lyr)
	aggHash := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(lyr.Blocks())), prevHash.Bytes())
	assert.Equal(t, aggHash, msh.GetAggregatedLayerHash(lyr.Index()))
	prevHash = aggHash

	// add a layer with some invalid blocks
	lyrID := gLyr.Add(1)
	lyr = addLayer(r, lyrID, 5, msh)
	r.Equal(5, len(lyr.Blocks()))
	r.NoError(msh.SaveContextualValidity(lyr.Blocks()[0].ID(), lyrID, false))
	msh.ValidateLayer(context.TODO(), lyr)
	// aggregated layer hash only includes valid blocks
	aggHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(lyr.Blocks()[1:])), prevHash.Bytes())
	assert.Equal(t, aggHash, msh.GetAggregatedLayerHash(lyr.Index()))
}

func TestMesh_ProcessedLayer(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "processed layer")
	t.Cleanup(func() {
		msh.Close()
	})

	// test genesis layers
	gLyr := types.GetEffectiveGenesis()
	var lyrs []*types.Layer
	for i := types.NewLayerID(1); i.Before(gLyr); i = i.Add(1) {
		lyr := addLayer(r, i, 0, msh)
		lyrs = append(lyrs, lyr)
	}
	prevHash := types.Hash32{}
	for _, lyr := range lyrs {
		assert.Equal(t, EmptyLayerHash, msh.GetLayerHash(lyr.Index()))
		msh.setProcessedLayer(lyr)
		expectedHash := types.CalcBlocksHash32([]types.BlockID{}, prevHash.Bytes())
		assert.Equal(t, lyr.Index(), msh.ProcessedLayer())
		assert.Equal(t, expectedHash, msh.ProcessedLayerHash())
		// make sure processed layer is persisted
		pLyr, err := msh.recoverProcessedLayer()
		assert.NoError(t, err)
		assert.Equal(t, lyr.Index(), pLyr.ID)
		assert.Equal(t, expectedHash, pLyr.Hash)
		prevHash = expectedHash
	}

	// effective genesis layer
	_, err := msh.recoverLayerHash(gLyr)
	assert.Equal(t, database.ErrNotFound, err)
	lyr, err := msh.GetLayer(gLyr)
	r.NoError(err)
	msh.setProcessedLayer(lyr)
	h := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(lyr.Blocks())), nil)
	assert.Equal(t, h, msh.GetLayerHash(lyr.Index()))
	expectedHash := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(lyr.Blocks())), prevHash.Bytes())
	assert.Equal(t, lyr.Index(), msh.ProcessedLayer())
	assert.Equal(t, expectedHash, msh.ProcessedLayerHash())
	// make sure processed layer is persisted
	pLyr, err := msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, lyr.Index(), pLyr.ID)
	assert.Equal(t, expectedHash, pLyr.Hash)
	prevHash = expectedHash

	gPlus1 := addLayer(r, gLyr.Add(1), 1, msh)
	gPlus2 := addLayer(r, gLyr.Add(2), 2, msh)
	gPlus3 := addLayer(r, gLyr.Add(3), 3, msh)
	gPlus4 := addLayer(r, gLyr.Add(4), 4, msh)
	gPlus5 := addLayer(r, gLyr.Add(5), 5, msh)

	_, err = msh.recoverLayerHash(gPlus1.Index())
	assert.Equal(t, database.ErrNotFound, err)
	msh.setProcessedLayer(gPlus1)
	h = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus1.Blocks())), nil)
	assert.Equal(t, h, msh.GetLayerHash(gPlus1.Index()))
	expectedHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus1.Blocks())), prevHash.Bytes())
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	assert.Equal(t, expectedHash, msh.ProcessedLayerHash())
	// make sure processed layer is persisted
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr.ID)
	assert.Equal(t, expectedHash, pLyr.Hash)
	prevHash = expectedHash

	// set gPlus3 and gPlus5 out of order
	_, err = msh.recoverLayerHash(gPlus3.Index())
	assert.Equal(t, database.ErrNotFound, err)
	msh.setProcessedLayer(gPlus3)
	h = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus3.Blocks())), nil)
	assert.Equal(t, h, msh.GetLayerHash(gPlus3.Index()))
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	assert.Equal(t, prevHash, msh.ProcessedLayerHash())
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr.ID)
	assert.Equal(t, expectedHash, pLyr.Hash)

	_, err = msh.recoverLayerHash(gPlus5.Index())
	assert.Equal(t, database.ErrNotFound, err)
	msh.setProcessedLayer(gPlus5)
	h = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus5.Blocks())), nil)
	assert.Equal(t, h, msh.GetLayerHash(gPlus5.Index()))
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	assert.Equal(t, prevHash, msh.ProcessedLayerHash())
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr.ID)
	assert.Equal(t, expectedHash, pLyr.Hash)

	// setting gPlus2 will bring the processed layer to gPlus3
	_, err = msh.recoverLayerHash(gPlus2.Index())
	assert.Equal(t, database.ErrNotFound, err)
	msh.setProcessedLayer(gPlus2)
	h = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus2.Blocks())), nil)
	assert.Equal(t, h, msh.GetLayerHash(gPlus2.Index()))
	gPlus2Hash := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus2.Blocks())), prevHash.Bytes())
	expectedHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus3.Blocks())), gPlus2Hash.Bytes())
	assert.Equal(t, gPlus3.Index(), msh.ProcessedLayer())
	assert.Equal(t, expectedHash, msh.ProcessedLayerHash())
	// make sure processed layer is persisted
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus3.Index(), pLyr.ID)
	assert.Equal(t, expectedHash, pLyr.Hash)
	prevHash = expectedHash

	// setting gPlus4 will bring the processed layer to gPlus5
	_, err = msh.recoverLayerHash(gPlus4.Index())
	assert.Equal(t, database.ErrNotFound, err)
	msh.setProcessedLayer(gPlus4)
	h = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus4.Blocks())), nil)
	assert.Equal(t, h, msh.GetLayerHash(gPlus4.Index()))
	gPlus4Hash := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus4.Blocks())), prevHash.Bytes())
	expectedHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus5.Blocks())), gPlus4Hash.Bytes())
	assert.Equal(t, gPlus5.Index(), msh.ProcessedLayer())
	assert.Equal(t, expectedHash, msh.ProcessedLayerHash())
	// make sure processed layer is persisted
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus5.Index(), pLyr.ID)
	assert.Equal(t, expectedHash, pLyr.Hash)
	prevHash = expectedHash

	// setting it to an older layer should have no effect
	h, err = msh.recoverLayerHash(gPlus2.Index())
	assert.NoError(t, err)
	msh.setProcessedLayer(gPlus2)
	assert.Equal(t, h, msh.GetLayerHash(gPlus2.Index()))
	assert.Equal(t, gPlus5.Index(), msh.ProcessedLayer())
	assert.Equal(t, prevHash, msh.ProcessedLayerHash())
	// make sure processed layer is persisted
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus5.Index(), pLyr.ID)
	assert.Equal(t, expectedHash, pLyr.Hash)

	// add a couple more blocks to gPlus3
	aggHash1, _ := msh.getAggregatedLayerHash(gPlus1.Index())
	aggHash2, _ := msh.getAggregatedLayerHash(gPlus2.Index())
	aggHash3, _ := msh.getAggregatedLayerHash(gPlus3.Index())
	aggHash4, _ := msh.getAggregatedLayerHash(gPlus4.Index())
	aggHash5, _ := msh.getAggregatedLayerHash(gPlus5.Index())
	gPlus3 = addLayer(r, gLyr.Add(3), 2, msh)
	oldHash, err := msh.recoverLayerHash(gPlus2.Index())
	assert.NoError(t, err)
	msh.setProcessedLayer(gPlus3)
	h = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(gPlus3.Blocks())), nil)
	assert.Equal(t, h, msh.GetLayerHash(gPlus3.Index()))
	assert.NotEqual(t, oldHash, msh.GetLayerHash(gPlus3.Index()))
	assert.Equal(t, gPlus5.Index(), msh.ProcessedLayer())
	for i, hash := range []types.Hash32{aggHash1, aggHash2, aggHash3, aggHash4, aggHash5} {
		lyr := gLyr.Add(uint32(i + 1))
		aggHash, _ := msh.getAggregatedLayerHash(lyr)
		if i < 2 {
			assert.Equal(t, hash, aggHash, i)
		} else {
			assert.NotEqual(t, hash, aggHash, i)
			layer, err := msh.GetLayer(lyr)
			r.NoError(err)
			expectedHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(layer.Blocks())), prevHash.Bytes())
			assert.Equal(t, expectedHash, aggHash)
		}
		if i == 5 {
			assert.Equal(t, hash, msh.ProcessedLayerHash())
		}
		prevHash = aggHash
	}
}

func TestMesh_PersistProcessedLayer(t *testing.T) {
	msh := getMesh(t, "persist_processed_layer")
	t.Cleanup(func() {
		msh.Close()
	})
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
	t.Cleanup(func() {
		msh.Close()
	})
	msh.setLatestLayer(types.NewLayerID(3))
	msh.setLatestLayer(types.NewLayerID(7))
	msh.setLatestLayer(types.NewLayerID(10))
	msh.setLatestLayer(types.NewLayerID(1))
	msh.setLatestLayer(types.NewLayerID(2))
	assert.Equal(t, types.NewLayerID(10), msh.LatestLayer(), "wrong layer")
}

func TestMesh_WakeUp(t *testing.T) {
	msh := getMesh(t, "t1")
	t.Cleanup(func() {
		msh.Close()
	})

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
	t.Cleanup(func() {
		msh.Close()
	})
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
	t.Cleanup(func() {
		msh.Close()
	})
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

	msh.pushLayersToState(context.TODO(), types.GetEffectiveGenesis(), types.GetEffectiveGenesis().Add(1))
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
	t.Cleanup(func() {
		msh.Close()
	})
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

func randomHash() (hash types.Hash32) {
	hash.SetBytes([]byte(rand.String(32)))
	return
}

func TestMesh_persistLayerHash(t *testing.T) {
	msh := getMesh(t, "persistLayerHash")
	t.Cleanup(func() {
		msh.Close()
	})

	// persist once
	lyr := types.NewLayerID(3)
	hash := randomHash()
	msh.persistLayerHash(lyr, hash)
	assert.Equal(t, hash, msh.GetLayerHash(lyr))

	// persist twice
	newHash := randomHash()
	assert.NotEqual(t, newHash, hash)
	msh.persistLayerHash(lyr, newHash)
	assert.Equal(t, newHash, msh.GetLayerHash(lyr))
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
	err := msh.SaveContextualValidity(blk.ID(), blk.LayerIndex, valid)
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
