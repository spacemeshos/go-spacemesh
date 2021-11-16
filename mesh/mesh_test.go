package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

type ContextualValidityMock struct{}

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
	mdb          *DB
	lastComplete types.LayerID
}

func (m *MeshValidatorMock) Persist(context.Context) error {
	return nil
}

func (m *MeshValidatorMock) LatestComplete() types.LayerID {
	panic("implement me")
}

func (m *MeshValidatorMock) HandleIncomingLayer(_ context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	gLyr := types.GetEffectiveGenesis()
	if !layerID.After(gLyr) {
		return gLyr, gLyr, false
	}
	oldVerified := layerID.Sub(2)
	if oldVerified.Before(gLyr) {
		oldVerified = gLyr
	}
	newVerified := layerID.Sub(1)
	return oldVerified, newVerified, false
}

func (m *MeshValidatorMock) HandleLateBlocks(_ context.Context, bl []*types.Block) (types.LayerID, types.LayerID) {
	return bl[0].Layer().Sub(1), bl[0].Layer()
}

type MockState struct{}

func (MockState) ValidateAndAddTxToPool(*types.Transaction, types.LayerID) error {
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

func (s *MockState) ApplyLayer(l types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
	return make([]*types.Transaction, 0), nil
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

func (m *MockTxMemPool) Put(id types.TransactionID, tx *types.Transaction, layerID types.LayerID) {
	m.db[id] = tx
}

func (MockTxMemPool) Invalidate(types.TransactionID, types.LayerID) {
}

func getMesh(tb testing.TB, id string) *Mesh {
	lg := logtest.New(tb).WithName(id)
	mmdb := NewMemMeshDB(lg)
	ctrl := gomock.NewController(tb)
	mockFetch := mocks.NewMockBlockFetcher(ctrl)
	mockFetch.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).AnyTimes()
	return NewMesh(mmdb, NewAtxDbMock(), ConfigTst(), mockFetch, &MeshValidatorMock{mdb: mmdb}, newMockTxMemPool(), &MockState{}, lg)
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

func TestMesh_LayerBlocksSortedByIDs(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "blockIDs")
	t.Cleanup(func() {
		msh.Close()
	})
	lyr := addLayer(r, types.GetEffectiveGenesis().Add(19), 100, msh)
	blockIDs := types.BlockIDs(lyr.Blocks())
	outOfSort := blockIDs[1:]
	outOfSort = append(outOfSort, blockIDs[0])
	sorted := types.SortBlockIDs(blockIDs)
	assert.Equal(t, sorted, blockIDs)
	assert.NotEqual(t, sorted, outOfSort)
}

func TestMesh_LayerHash(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "layerHash")
	t.Cleanup(func() {
		msh.Close()
	})

	gLyr := types.GetEffectiveGenesis()
	latestLyr := gLyr.Add(5)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		numBlocks := 10 * i.Uint32()
		lyr := addLayer(r, i, int(numBlocks), msh)
		// make the first block of each layer invalid
		msh.SaveContextualValidity(lyr.Blocks()[0].ID(), lyr.Index(), false)
	}

	for i := types.NewLayerID(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := msh.GetLayer(i)
		r.NoError(err)
		if !i.After(gLyr) {
			var expHash types.Hash32
			if i == gLyr {
				expHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(thisLyr.Blocks())), nil)
			} else {
				expHash = types.EmptyLayerHash
			}
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			assert.Equal(t, expHash, msh.GetLayerHash(i))
			msh.ValidateLayer(context.TODO(), thisLyr)
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			assert.Equal(t, expHash, msh.GetLayerHash(i))
		} else {
			// when tortoise verify a layer N, it can only determine blocks' contextual validity for layer N-1
			prevLyr, err := msh.GetLayer(i.Sub(1))
			r.NoError(err)
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			assert.Equal(t, prevLyr.Hash(), msh.GetLayerHash(i.Sub(1)))
			msh.ValidateLayer(context.TODO(), thisLyr)
			// for this layer, hash is unchanged because the block contextual validity is not determined yet
			assert.Equal(t, thisLyr.Hash(), msh.GetLayerHash(i))
			if prevLyr.Index().After(gLyr) {
				// but for previous layer hash should already be changed to contain only valid blocks
				prevExpHash := types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(prevLyr.Blocks()[1:])), nil)
				assert.Equal(t, prevExpHash, msh.GetLayerHash(i.Sub(1)))
				assert.NotEqual(t, prevLyr.Hash(), msh.GetLayerHash(i.Sub(1)))
			}
		}
	}
}

func TestMesh_GetAggregatedLayerHash(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "aggLayerHash")
	t.Cleanup(func() {
		msh.Close()
	})

	gLyr := types.GetEffectiveGenesis()
	latestLyr := gLyr.Add(5)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		numBlocks := 10 * i.Uint32()
		lyr := addLayer(r, i, int(numBlocks), msh)
		// make the first block of each layer invalid
		msh.SaveContextualValidity(lyr.Blocks()[0].ID(), lyr.Index(), false)
	}

	prevAggHash := types.EmptyLayerHash
	var expHash types.Hash32
	for i := types.NewLayerID(1); !i.After(gLyr); i = i.Add(1) {
		thisLyr, err := msh.GetLayer(i)
		r.NoError(err)
		if i == gLyr {
			expHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(thisLyr.Blocks())), prevAggHash.Bytes())
		} else {
			expHash = types.CalcBlocksHash32([]types.BlockID{}, prevAggHash.Bytes())
		}
		assert.Equal(t, expHash, msh.GetAggregatedLayerHash(i))
		msh.ValidateLayer(context.TODO(), thisLyr)
		assert.Equal(t, expHash, msh.GetAggregatedLayerHash(i))
		prevAggHash = expHash
	}

	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := msh.GetLayer(i)
		r.NoError(err)
		prevLyr, err := msh.GetLayer(i.Sub(1))
		r.NoError(err)
		if prevLyr.Index() == gLyr {
			r.Equal(prevAggHash, msh.GetAggregatedLayerHash(i.Sub(1)))
			r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
			msh.ValidateLayer(context.TODO(), thisLyr)
			r.Equal(prevAggHash, msh.GetAggregatedLayerHash(i.Sub(1)))
			r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
			continue
		}
		r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i.Sub(1)))
		r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
		msh.ValidateLayer(context.TODO(), thisLyr)
		// contextual validity is still not determined for thisLyr, so aggregated hash is not calculated for this layer
		r.Equal(types.EmptyLayerHash, msh.GetAggregatedLayerHash(i))
		// but for previous layer hash should already be changed to contain only valid blocks
		expHash = types.CalcBlocksHash32(types.SortBlockIDs(types.BlockIDs(prevLyr.Blocks()[1:])), prevAggHash.Bytes())
		assert.Equal(t, expHash, msh.GetAggregatedLayerHash(prevLyr.Index()))
		prevAggHash = expHash
	}
}

func TestMesh_SetZeroBlockLayer(t *testing.T) {
	r := require.New(t)
	msh := getMesh(t, "zero block layer")
	t.Cleanup(func() {
		msh.Close()
	})

	lyrID := types.GetEffectiveGenesis().Add(1)
	lyr, err := msh.GetLayer(lyrID)
	r.ErrorIs(err, database.ErrNotFound)
	r.Nil(lyr)
	err = msh.SetZeroBlockLayer(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, types.EmptyLayerHash, msh.GetLayerHash(lyrID))

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

	id := types.GetEffectiveGenesis().Add(1)
	_, err := msh.GetLayer(id)
	r.ErrorIs(err, database.ErrNotFound)

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
	lyr, err := msh.GetLayer(gLyr)
	r.NoError(err)
	lyrs = append(lyrs, lyr)
	for _, lyr := range lyrs {
		msh.ValidateLayer(context.TODO(), lyr)
		assert.Equal(t, lyr.Index(), msh.ProcessedLayer())
		// make sure processed layer is persisted
		pLyr, err := msh.recoverProcessedLayer()
		assert.NoError(t, err)
		assert.Equal(t, lyr.Index(), pLyr)
	}

	gPlus1 := addLayer(r, gLyr.Add(1), 1, msh)
	gPlus2 := addLayer(r, gLyr.Add(2), 2, msh)
	gPlus3 := addLayer(r, gLyr.Add(3), 3, msh)
	gPlus4 := addLayer(r, gLyr.Add(4), 4, msh)
	gPlus5 := addLayer(r, gLyr.Add(5), 5, msh)

	msh.ValidateLayer(context.TODO(), gPlus1)
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err := msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	// set gPlus3 and gPlus5 out of order
	msh.ValidateLayer(context.TODO(), gPlus3)
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	msh.ValidateLayer(context.TODO(), gPlus5)
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), msh.ProcessedLayer())
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	// setting gPlus2 will bring the processed layer to gPlus3
	msh.ValidateLayer(context.TODO(), gPlus2)
	assert.Equal(t, gPlus3.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus3.Index(), pLyr)

	// setting gPlus4 will bring the processed layer to gPlus5
	msh.ValidateLayer(context.TODO(), gPlus4)
	assert.Equal(t, gPlus5.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus5.Index(), pLyr)

	// setting it to an older layer should have no effect
	msh.ValidateLayer(context.TODO(), gPlus2)
	assert.Equal(t, gPlus5.Index(), msh.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = msh.recoverProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus5.Index(), pLyr)
}

func TestMesh_PersistProcessedLayer(t *testing.T) {
	msh := getMesh(t, "persist_processed_layer")
	t.Cleanup(func() {
		msh.Close()
	})
	layerID := types.NewLayerID(3)
	assert.NoError(t, msh.persistProcessedLayer(layerID))
	rLyr, err := msh.recoverProcessedLayer()
	assert.NoError(t, err)
	assert.Equal(t, layerID, rLyr)
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

	gLyr := types.GetEffectiveGenesis()
	r := require.New(t)
	txIDs1, _ := addManyTXsToPool(r, msh, 4)
	txIDs2, _ := addManyTXsToPool(r, msh, 3)
	block1 := types.NewExistingBlock(gLyr.Add(1), []byte("data1"), txIDs1)
	block2 := types.NewExistingBlock(gLyr.Add(2), []byte("data2"), txIDs2)

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

	ctrl := gomock.NewController(t)
	recoveredMesh := NewMesh(msh.DB, NewAtxDbMock(), ConfigTst(), mocks.NewMockBlockFetcher(ctrl), &MeshValidatorMock{mdb: msh.DB}, newMockTxMemPool(), &MockState{}, logtest.New(t))

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
	msh.state = state

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
	msh.state = state

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
		msh.txPool.Put(tx.ID(), tx, types.LayerID{})
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
		msh.txPool.Put(tx.ID(), tx, types.LayerID{})
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

func TestMesh_HandleValidatedLayer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFetch := mocks.NewMockBlockFetcher(ctrl)
	numOfBlocks := 10
	maxTxs := 20
	s := &MockMapState{Rewards: make(map[types.Address]uint64)}
	msh, atxDB := getMeshWithMapState(t, "t1", s)
	defer msh.Close()
	msh.fetch = mockFetch

	gLyr := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); i.Before(gLyr); i = i.Add(1) {
		msh.HandleValidatedLayer(context.TODO(), i, []types.BlockID{})
		require.Equal(t, i, msh.ProcessedLayer())
	}
	msh.HandleValidatedLayer(context.TODO(), gLyr, []types.BlockID{GenesisBlock().ID()})
	require.Equal(t, gLyr, msh.ProcessedLayer())

	lyr := gLyr.Add(1)
	_, blocks := createLayer(t, msh, lyr, numOfBlocks, maxTxs, atxDB)
	mockFetch.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Do(
		func(ctx context.Context, blockIDs []types.BlockID) {
			assert.Equal(t, types.SortBlockIDs(blockIDs), types.SortBlockIDs(types.BlockIDs(blocks)))
		}).Times(1)
	msh.HandleValidatedLayer(context.TODO(), lyr, types.BlockIDs(blocks))
	require.Equal(t, lyr, msh.ProcessedLayer())
}

func TestMesh_HandleValidatedLayer_emptyOutputFromHare(t *testing.T) {
	msh := getMesh(t, "HandleValidatedLayer_Empty")
	layerID := types.GetEffectiveGenesis().Add(1)

	createMeshFromHareOutput(t, layerID, msh, NewAtxDbMock())
	require.Equal(t, layerID, msh.ProcessedLayer())

	var empty []types.BlockID
	layerID = layerID.Add(1)
	msh.HandleValidatedLayer(context.TODO(), layerID, empty)

	// input vector saved
	iv, err := msh.GetLayerInputVectorByID(layerID)
	require.NoError(t, err)
	assert.Nil(t, iv)

	// but processed layer has advanced
	assert.Equal(t, layerID, msh.ProcessedLayer())
}
