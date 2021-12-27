package mesh

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	numOfBlocks = 10
	maxTxs      = 20
)

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

type testMesh struct {
	*Mesh
	ctrl         *gomock.Controller
	mockFetch    *smocks.MockFetcher
	mockState    *mocks.Mockstate
	mockTortoise *mocks.Mocktortoise

	mockATXDB *AtxDbMock
}

func createTestMesh(tb testing.TB) *testMesh {
	lg := logtest.New(tb)
	mmdb := NewMemMeshDB(lg)
	ctrl := gomock.NewController(tb)
	tm := &testMesh{
		ctrl:         ctrl,
		mockFetch:    smocks.NewMockFetcher(ctrl),
		mockState:    mocks.NewMockstate(ctrl),
		mockTortoise: mocks.NewMocktortoise(ctrl),
		mockATXDB:    NewAtxDbMock(),
	}
	tm.Mesh = NewMesh(mmdb, tm.mockATXDB, ConfigTst(), tm.mockFetch, tm.mockTortoise, newMockTxMemPool(), tm.mockState, lg)
	return tm
}

func TestMesh_LayerBlocksSortedByIDs(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	_, blocks := createLayer(t, tm.Mesh, types.GetEffectiveGenesis().Add(19), 100, maxTxs, tm.mockATXDB)
	blockIDs := types.BlockIDs(blocks)
	outOfSort := blockIDs[1:]
	outOfSort = append(outOfSort, blockIDs[0])
	sorted := types.SortBlockIDs(blockIDs)
	assert.Equal(t, sorted, blockIDs)
	assert.NotEqual(t, sorted, outOfSort)
}

func TestMesh_LayerHash(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		_, blocks := createLayer(t, tm.Mesh, i, numOfBlocks, maxTxs, tm.mockATXDB)
		require.NoError(t, tm.SaveContextualValidity(types.SortBlocks(blocks)[0].ID(), i, false))
	}

	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(numLayers - 1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		r.NoError(err)
		// when tortoise verify a layer N, it can only determine blocks' contextual validity for layer N-1
		prevLyr, err := tm.GetLayer(i.Sub(1))
		r.NoError(err)
		assert.Equal(t, thisLyr.Hash(), tm.GetLayerHash(i))
		assert.Equal(t, prevLyr.Hash(), tm.GetLayerHash(i.Sub(1)))

		if prevLyr.Index().After(gLyr) {
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(2), i.Sub(1), false).Times(1)
			tm.mockState.EXPECT().ApplyLayer(prevLyr.Index(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
		} else {
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(gLyr, gLyr, false).Times(1)
		}
		tm.ProcessLayer(context.TODO(), thisLyr)
		// for this layer, hash is unchanged because the block contextual validity is not determined yet
		assert.Equal(t, thisLyr.Hash(), tm.GetLayerHash(i))
		if prevLyr.Index().After(gLyr) {
			// but for previous layer hash should already be changed to contain only valid proposals
			prevExpHash := types.CalcBlocksHash32(types.SortBlockIDs(prevLyr.BlocksIDs()[1:]), nil)
			assert.Equal(t, prevExpHash, tm.GetLayerHash(i.Sub(1)))
			assert.NotEqual(t, prevLyr.Hash(), tm.GetLayerHash(i.Sub(1)))
		}
	}
}

func TestMesh_GetAggregatedLayerHash(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		_, blocks := createLayer(t, tm.Mesh, i, numOfBlocks, maxTxs, tm.mockATXDB)
		require.NoError(t, tm.SaveContextualValidity(types.SortBlocks(blocks)[0].ID(), i, false))
	}

	prevAggHash := tm.GetAggregatedLayerHash(gLyr)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(numLayers - 1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		r.NoError(err)
		prevLyr, err := tm.GetLayer(i.Sub(1))
		r.NoError(err)
		if prevLyr.Index() == gLyr {
			r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(gLyr, gLyr, false).Times(1)
			tm.ProcessLayer(context.TODO(), thisLyr)
			r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
			continue
		}
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(2), i.Sub(1), false).Times(1)
		tm.mockState.EXPECT().ApplyLayer(prevLyr.Index(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i.Sub(1)))
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
		tm.ProcessLayer(context.TODO(), thisLyr)
		// contextual validity is still not determined for thisLyr, so aggregated hash is not calculated for this layer
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
		// but for previous layer hash should already be changed to contain only valid proposals
		expHash := types.CalcBlocksHash32(types.SortBlockIDs(prevLyr.BlocksIDs()[1:]), prevAggHash.Bytes())
		assert.Equal(t, expHash, tm.GetAggregatedLayerHash(prevLyr.Index()))
		prevAggHash = expHash
	}
}

func TestMesh_SetZeroBlockLayer(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	lyrID := types.GetEffectiveGenesis().Add(1)
	lyr, err := tm.GetLayer(lyrID)
	r.ErrorIs(err, database.ErrNotFound)
	r.Nil(lyr)
	err = tm.SetZeroBlockLayer(lyrID)
	assert.NoError(t, err)
	assert.Equal(t, types.EmptyLayerHash, tm.GetLayerHash(lyrID))

	// it's ok to add to an empty layer
	_, blocks := createLayer(t, tm.Mesh, lyrID, numOfBlocks, maxTxs, tm.mockATXDB)
	lyr, err = tm.GetLayer(lyrID)
	r.NoError(err)
	assert.ElementsMatch(t, types.SortBlockIDs(types.BlockIDs(blocks)), types.SortBlockIDs(lyr.BlocksIDs()))

	// but not okay to set a non-empty layer to an empty layer
	err = tm.SetZeroBlockLayer(lyrID)
	assert.Equal(t, errLayerHasBlock, err)
}

func TestMesh_AddLayerGetLayer(t *testing.T) {
	r := require.New(t)

	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	id := types.GetEffectiveGenesis().Add(1)
	_, err := tm.GetLayer(id)
	r.ErrorIs(err, database.ErrNotFound)

	txIDs1, _ := addManyTXsToPool(r, tm.Mesh, 4)
	txIDs2, _ := addManyTXsToPool(r, tm.Mesh, 3)
	txIDs3, _ := addManyTXsToPool(r, tm.Mesh, 6)
	r.NoError(tm.AddProposalWithTxs(context.TODO(), types.GenLayerProposal(id, txIDs1)))
	r.NoError(tm.AddProposalWithTxs(context.TODO(), types.GenLayerProposal(id, txIDs2)))
	r.NoError(tm.AddProposalWithTxs(context.TODO(), types.GenLayerProposal(id, txIDs3)))

	lyr, err := tm.GetLayer(id)
	r.NoError(err)
	r.Equal(id, lyr.Index())
	r.Equal(3, len(lyr.Proposals()))
}

func TestMesh_ProcessedLayer(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	for i := types.NewLayerID(1); !i.After(gLyr); i = i.Add(1) {
		lyr, err := tm.GetLayer(i)
		require.NoError(t, err)
		tm.setProcessedLayer(lyr)
	}
	_, blocks1 := createLayer(t, tm.Mesh, gLyr.Add(1), 1*numOfBlocks, maxTxs, tm.mockATXDB)
	_, blocks2 := createLayer(t, tm.Mesh, gLyr.Add(2), 2*numOfBlocks, maxTxs, tm.mockATXDB)
	_, blocks3 := createLayer(t, tm.Mesh, gLyr.Add(3), 3*numOfBlocks, maxTxs, tm.mockATXDB)
	_, blocks4 := createLayer(t, tm.Mesh, gLyr.Add(4), 4*numOfBlocks, maxTxs, tm.mockATXDB)
	_, blocks5 := createLayer(t, tm.Mesh, gLyr.Add(5), 5*numOfBlocks, maxTxs, tm.mockATXDB)
	gPlus1 := types.NewExistingLayer(gLyr.Add(1), blocks1)
	gPlus2 := types.NewExistingLayer(gLyr.Add(2), blocks2)
	gPlus3 := types.NewExistingLayer(gLyr.Add(3), blocks3)
	gPlus4 := types.NewExistingLayer(gLyr.Add(4), blocks4)
	gPlus5 := types.NewExistingLayer(gLyr.Add(5), blocks5)

	tm.setProcessedLayer(gPlus1)
	assert.Equal(t, gPlus1.Index(), tm.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err := tm.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	// set gPlus3 and gPlus5 out of order
	tm.setProcessedLayer(gPlus3)
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), tm.ProcessedLayer())
	pLyr, err = tm.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	tm.setProcessedLayer(gPlus5)
	// processed layer should not advance
	assert.Equal(t, gPlus1.Index(), tm.ProcessedLayer())
	pLyr, err = tm.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus1.Index(), pLyr)

	// setting gPlus2 will bring the processed layer to gPlus3
	tm.setProcessedLayer(gPlus2)
	assert.Equal(t, gPlus3.Index(), tm.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = tm.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus3.Index(), pLyr)

	// setting gPlus4 will bring the processed layer to gPlus5
	tm.setProcessedLayer(gPlus4)
	assert.Equal(t, gPlus5.Index(), tm.ProcessedLayer())
	// make sure processed layer is persisted
	pLyr, err = tm.GetProcessedLayer()
	r.NoError(err)
	assert.Equal(t, gPlus5.Index(), pLyr)

	// setting it to an older layer should have no effect
	tm.setProcessedLayer(gPlus2)
	assert.Equal(t, gPlus5.Index(), tm.ProcessedLayer())
}

func TestMesh_PersistProcessedLayer(t *testing.T) {
	tm := createTestMesh(t)
	t.Cleanup(func() {
		tm.Close()
	})
	layerID := types.NewLayerID(3)
	assert.NoError(t, tm.persistProcessedLayer(layerID))
	rLyr, err := tm.GetProcessedLayer()
	assert.NoError(t, err)
	assert.Equal(t, layerID, rLyr)
}

func TestMesh_LatestKnownLayer(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	tm.setLatestLayer(types.NewLayerID(3))
	tm.setLatestLayer(types.NewLayerID(7))
	tm.setLatestLayer(types.NewLayerID(10))
	tm.setLatestLayer(types.NewLayerID(1))
	tm.setLatestLayer(types.NewLayerID(2))
	assert.Equal(t, types.NewLayerID(10), tm.LatestLayer(), "wrong layer")
}

func TestMesh_WakeUp(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	r := require.New(t)
	txIDs1, _ := addManyTXsToPool(r, tm.Mesh, 4)
	txIDs2, _ := addManyTXsToPool(r, tm.Mesh, 3)
	block1 := types.GenLayerProposal(gLyr.Add(1), txIDs1)
	block2 := types.GenLayerProposal(gLyr.Add(2), txIDs2)

	assert.NoError(t, tm.AddProposalWithTxs(context.TODO(), block1))
	assert.NoError(t, tm.AddProposalWithTxs(context.TODO(), block2))

	rBlock2, err := tm.getProposal(block2.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs2), len(rBlock2.TxIDs), "block TX size was wrong")

	rBlock1, err := tm.getProposal(block1.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs1), len(rBlock1.TxIDs), "block TX size was wrong")

	recoveredMesh := NewMesh(tm.DB, NewAtxDbMock(), ConfigTst(), tm.mockFetch, tm.mockTortoise, newMockTxMemPool(), tm.mockState, logtest.New(t))

	rBlock2, err = recoveredMesh.getProposal(block2.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs2), len(rBlock2.TxIDs), "block TX size was wrong")

	rBlock1, err = recoveredMesh.getProposal(block1.ID())
	assert.NoError(t, err)
	assert.Equal(t, len(txIDs1), len(rBlock1.TxIDs), "block TX size was wrong")
}

func TestMesh_AddBlockWithTxs_PushTransactions_UpdateUnappliedTxs(t *testing.T) {
	r := require.New(t)

	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	layerID := types.GetEffectiveGenesis().Add(1)
	signer, origin := newSignerAndAddress(r, "origin")
	tx1 := addTxToMesh(r, tm.Mesh, signer, 2468)
	tx2 := addTxToMesh(r, tm.Mesh, signer, 2469)
	tx3 := addTxToMesh(r, tm.Mesh, signer, 2470)
	tx4 := addTxToMesh(r, tm.Mesh, signer, 2471)
	tx5 := addTxToMesh(r, tm.Mesh, signer, 2472)
	addBlockAndTxsToMesh(r, tm.Mesh, layerID, true, tx1, tx2)
	addBlockAndTxsToMesh(r, tm.Mesh, layerID, true, tx2, tx3, tx4)
	addBlockAndTxsToMesh(r, tm.Mesh, layerID, false, tx4, tx5)
	addBlockAndTxsToMesh(r, tm.Mesh, layerID, false, tx5)

	txns := getTxns(r, tm.DB, origin)
	r.Len(txns, 5)
	for i := 0; i < 5; i++ {
		r.Equal(2468+i, int(txns[i].Nonce))
		r.Equal(111, int(txns[i].TotalAmount))
	}

	var allTXs []*types.Transaction
	allRewards := make(map[types.Address]uint64)
	tm.mockState.EXPECT().ApplyLayer(layerID, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ types.LayerID, txs []*types.Transaction, rewards map[types.Address]uint64) ([]*types.Transaction, error) {
			allTXs = append(allTXs, txs...)
			for addr, reward := range rewards {
				allRewards[addr] += reward
			}
			return nil, nil
		}).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(tx5).Return(nil).Times(1)
	tm.mockState.EXPECT().AddTxToPool(tx5).Return(nil).Times(1)
	tm.pushLayersToState(context.TODO(), types.GetEffectiveGenesis(), types.GetEffectiveGenesis().Add(1))
	r.Equal(4, len(allTXs))

	txns = getTxns(r, tm.DB, origin)
	r.Empty(txns)
}

func TestMesh_ExtractUniqueOrderedTransactions(t *testing.T) {
	r := require.New(t)

	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	layerID := types.NewLayerID(1)
	signer, _ := newSignerAndAddress(r, "origin")
	tx1 := addTxToMesh(r, tm.Mesh, signer, 2468)
	tx2 := addTxToMesh(r, tm.Mesh, signer, 2469)
	tx3 := addTxToMesh(r, tm.Mesh, signer, 2470)
	tx4 := addTxToMesh(r, tm.Mesh, signer, 2471)
	addBlockAndTxsToMesh(r, tm.Mesh, layerID, true, tx1, tx2)
	addBlockAndTxsToMesh(r, tm.Mesh, layerID, true, tx2, tx3, tx4)
	addBlockAndTxsToMesh(r, tm.Mesh, layerID, false, tx4)
	l, err := tm.GetLayer(layerID)
	r.NoError(err)

	validBlocks := extractUniqueOrderedTransactions(tm.Log, l, tm.DB)

	r.ElementsMatch(toTransactionIds(tx1, tx2, tx3, tx4), toTransactionIds(validBlocks...))
}

func randomHash() (hash types.Hash32) {
	hash.SetBytes([]byte(rand.String(32)))
	return
}

func TestMesh_persistLayerHash(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	// persist once
	lyr := types.NewLayerID(3)
	hash := randomHash()
	tm.persistLayerHash(lyr, hash)
	assert.Equal(t, hash, tm.GetLayerHash(lyr))

	// persist twice
	newHash := randomHash()
	assert.NotEqual(t, newHash, hash)
	tm.persistLayerHash(lyr, newHash)
	assert.Equal(t, newHash, tm.GetLayerHash(lyr))
}

func toTransactionIds(txs ...*types.Transaction) []types.TransactionID {
	var res []types.TransactionID
	for _, tx := range txs {
		res = append(res, tx.ID())
	}
	return res
}

func addTxToMesh(r *require.Assertions, msh *Mesh, signer *signing.EdSigner, nonce uint64) *types.Transaction {
	tx1 := newTx(r, signer, nonce, 111)
	p := &types.Proposal{}
	p.LayerIndex = types.NewLayerID(1)
	err := msh.writeTransactions(p, tx1)
	r.NoError(err)
	return tx1
}

func addBlockAndTxsToMesh(r *require.Assertions, msh *Mesh, id types.LayerID, valid bool, txs ...*types.Transaction) *types.Block {
	txIDs := make([]types.TransactionID, 0, len(txs))
	for _, tx := range txs {
		msh.txPool.Put(tx.ID(), tx)
		txIDs = append(txIDs, tx.ID())
	}
	b := types.GenLayerBlock(id, txIDs)
	b.Initialize()
	r.NoError(msh.SaveContextualValidity(b.ID(), id, valid))
	r.NoError(msh.AddProposalWithTxs(context.TODO(), (*types.Proposal)(b)))
	return b
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
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	numTXs := 6
	txIDs, _ := addManyTXsToPool(r, tm.Mesh, numTXs)

	block := types.GenLayerProposal(types.NewLayerID(1), txIDs)
	r.NoError(tm.AddProposalWithTxs(context.TODO(), block))

	res, err := tm.getProposal(block.ID())
	assert.NoError(t, err)
	assert.Equal(t, numTXs, len(res.TxIDs), "block TX size was wrong")
}

func TestMesh_HandleValidatedLayer(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	createMeshFromHareOutput(t, gPlus1, tm, tm.mockATXDB)
	require.Equal(t, gPlus1, tm.ProcessedLayer())

	gPlus2 := gLyr.Add(2)
	_, blocks := createLayer(t, tm.Mesh, gPlus2, 10, 100, tm.mockATXDB)
	blockIDs := types.BlockIDs(blocks)
	tm.mockFetch.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Do(
		func(_ context.Context, blockIDs []types.BlockID) {
			assert.Equal(t, types.SortBlockIDs(blockIDs), types.SortBlockIDs(blockIDs))
		}).Times(1)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus1, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blockIDs)

	// input vector saved
	iv, err := tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, blockIDs, iv)

	// but processed layer has advanced
	assert.Equal(t, gPlus2, tm.ProcessedLayer())
}

func TestMesh_HandleValidatedLayer_emptyOutputFromHare(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	createMeshFromHareOutput(t, gPlus1, tm, tm.mockATXDB)
	require.Equal(t, gPlus1, tm.ProcessedLayer())

	var empty []types.BlockID
	gPlus2 := gLyr.Add(2)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus1, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ValidateNonceAndBalance(gomock.Any()).Return(nil).AnyTimes()
	tm.mockState.EXPECT().AddTxToPool(gomock.Any()).Return(nil).AnyTimes()
	tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, empty)

	// input vector saved
	iv, err := tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Nil(t, iv)

	// but processed layer has advanced
	assert.Equal(t, gPlus2, tm.ProcessedLayer())
}
