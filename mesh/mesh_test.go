package mesh

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
)

const (
	numBallots = 10
	numBlocks  = 5
	numTXs     = 20
)

type testMesh struct {
	*Mesh
	ctrl         *gomock.Controller
	mockState    *mocks.MockconservativeState
	mockTortoise *mocks.Mocktortoise
}

func createTestMesh(t *testing.T) *testMesh {
	t.Helper()
	types.SetLayersPerEpoch(3)
	lg := logtest.New(t)
	mmdb := NewMemMeshDB(lg)
	ctrl := gomock.NewController(t)
	tm := &testMesh{
		ctrl:         ctrl,
		mockState:    mocks.NewMockconservativeState(ctrl),
		mockTortoise: mocks.NewMocktortoise(ctrl),
	}
	tm.Mesh = NewMesh(mmdb, nil, tm.mockTortoise, tm.mockState, lg)
	return tm
}

func createBlock(t testing.TB, mesh *Mesh, layerID types.LayerID, nodeID types.NodeID) *types.Block {
	t.Helper()
	coinbase := types.HexToAddress(nodeID.Key)
	txIDs := types.RandomTXSet(numTXs)
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			Rewards: []types.AnyReward{
				{
					Address:     coinbase,
					SmesherID:   nodeID,
					Amount:      unitReward,
					LayerReward: unitLayerReward,
				},
			},
			TxIDs: txIDs,
		},
	}
	b.Initialize()
	require.NoError(t, mesh.AddBlock(b))
	require.NoError(t, mesh.SaveContextualValidity(b.ID(), layerID, true))
	return b
}

func createLayerBallotsAndBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID) ([]*types.Ballot, []*types.Block) {
	t.Helper()
	return createLayerBallots(t, mesh, lyrID), createLayerBlocks(t, mesh, lyrID)
}

func createLayerBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID) []*types.Block {
	t.Helper()
	blocks := make([]*types.Block, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		nodeID := types.NodeID{Key: strconv.Itoa(i), VRFPublicKey: []byte("bbbbb")}
		blk := createBlock(t, mesh, lyrID, nodeID)
		blocks = append(blocks, blk)
	}
	return blocks
}

func createLayerBallots(t *testing.T, mesh *Mesh, lyrID types.LayerID) []*types.Ballot {
	t.Helper()
	ballots := make([]*types.Ballot, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballot := types.GenLayerBallot(lyrID)
		ballots = append(ballots, ballot)
		require.NoError(t, mesh.DB.AddBallot(ballot))
	}
	return ballots
}

func TestMesh_LayerHash(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		_, blocks := createLayerBallotsAndBlocks(t, tm.Mesh, i)
		require.NoError(t, tm.SaveContextualValidity(types.SortBlocks(blocks)[0].ID(), i, false))
	}

	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(numLayers - 1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).AnyTimes()
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		require.NoError(t, err)
		// when tortoise verify a layer N, it can only determine blocks' contextual validity for layer N-1
		prevLyr, err := tm.GetLayer(i.Sub(1))
		require.NoError(t, err)
		assert.Equal(t, thisLyr.Hash(), tm.GetLayerHash(i))
		assert.Equal(t, prevLyr.Hash(), tm.GetLayerHash(i.Sub(1)))

		if prevLyr.Index().After(gLyr) {
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(2), i.Sub(1), false).Times(1)
			tm.mockState.EXPECT().ApplyLayer(prevLyr.Index(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
		} else {
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(gLyr, gLyr, false).Times(1)
		}
		require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
		// for this layer, hash is unchanged because the block contextual validity is not determined yet
		assert.Equal(t, thisLyr.Hash(), tm.GetLayerHash(i))
		if prevLyr.Index().After(gLyr) {
			// but for previous layer hash should already be changed to contain only valid proposals
			prevExpHash := types.CalcBlocksHash32(types.SortBlockIDs(prevLyr.BlocksIDs())[1:], nil)
			require.Equal(t, prevExpHash, tm.GetLayerHash(i.Sub(1)), "layer %s", i.Sub(1))
			assert.NotEqual(t, prevLyr.Hash(), tm.GetLayerHash(i.Sub(1)))
		}
	}
}

func TestMesh_GetAggregatedLayerHash(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		_, blocks := createLayerBallotsAndBlocks(t, tm.Mesh, i)
		require.NoError(t, tm.SaveContextualValidity(types.SortBlocks(blocks)[0].ID(), i, false))
	}

	prevAggHash := tm.GetAggregatedLayerHash(gLyr)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(numLayers - 1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).AnyTimes()
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		r.NoError(err)
		prevLyr, err := tm.GetLayer(i.Sub(1))
		r.NoError(err)
		if prevLyr.Index() == gLyr {
			r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
			tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(gLyr, gLyr, false).Times(1)
			require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
			r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
			continue
		}
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(2), i.Sub(1), false).Times(1)
		tm.mockState.EXPECT().ApplyLayer(prevLyr.Index(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i.Sub(1)))
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
		require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
		// contextual validity is still not determined for thisLyr, so aggregated hash is not calculated for this layer
		r.Equal(types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
		// but for previous layer hash should already be changed to contain only valid proposals
		expHash := types.CalcBlocksHash32(types.SortBlockIDs(prevLyr.BlocksIDs())[1:], prevAggHash.Bytes())
		assert.Equal(t, expHash, tm.GetAggregatedLayerHash(prevLyr.Index()))
		prevAggHash = expHash
	}
}

func TestMesh_GetLayer(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	id := types.GetEffectiveGenesis().Add(1)
	lyr, err := tm.GetLayer(id)
	require.NoError(t, err)
	assert.Empty(t, lyr.Blocks())
	assert.Empty(t, lyr.Ballots())

	blocks := createLayerBlocks(t, tm.Mesh, id)
	ballots := createLayerBallots(t, tm.Mesh, id)
	lyr, err = tm.GetLayer(id)
	require.NoError(t, err)
	require.Equal(t, id, lyr.Index())
	require.Len(t, lyr.Ballots(), len(ballots))
	assert.ElementsMatch(t, ballots, lyr.Ballots())
	assert.ElementsMatch(t, blocks, lyr.Blocks())
}

func TestMesh_ProcessLayerPerHareOutput(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	_, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(1))
	_, blocks2 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(2))
	_, blocks3 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(3))
	_, blocks4 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(4))
	_, blocks5 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(5))

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	assert.Equal(t, blocks1[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus1, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blocks2[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, blocks2[0].ID(), hareOutput)
	assert.Equal(t, gPlus2, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus3).Return(gPlus1, gPlus2, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus2, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus3, blocks3[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus3)
	require.NoError(t, err)
	assert.Equal(t, blocks3[0].ID(), hareOutput)
	assert.Equal(t, gPlus3, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus2, gPlus3, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus3, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus4)
	require.NoError(t, err)
	assert.Equal(t, blocks4[0].ID(), hareOutput)
	assert.Equal(t, gPlus4, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gPlus3, gPlus4, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus4, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus5)
	require.NoError(t, err)
	assert.Equal(t, blocks5[0].ID(), hareOutput)
	assert.Equal(t, gPlus5, tm.ProcessedLayer())
}

func TestMesh_ProcessLayerPerHareOutput_OutOfOrder(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	_, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(1))
	_, blocks2 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(2))
	_, blocks3 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(3))
	_, blocks4 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(4))
	_, blocks5 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(5))

	// process order is  : gPlus1, gPlus3, gPlus5, gPlus2, gPlus4
	// processed layer is: gPlus1, gPlus1, gPlus1, gPlus3, gPlus5
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	assert.Equal(t, blocks1[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus3).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus3, blocks3[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus3)
	require.NoError(t, err)
	assert.Equal(t, blocks3[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus5)
	require.NoError(t, err)
	assert.Equal(t, blocks5[0].ID(), hareOutput)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus2, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus2, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(2)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).Times(2)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blocks2[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, blocks2[0].ID(), hareOutput)
	assert.Equal(t, gPlus3, tm.ProcessedLayer())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus2, gPlus4, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus3, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus4, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(2)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).Times(2)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	hareOutput, err = tm.GetHareConsensusOutput(gPlus4)
	require.NoError(t, err)
	assert.Equal(t, blocks4[0].ID(), hareOutput)
	assert.Equal(t, gPlus5, tm.ProcessedLayer())
}

func TestMesh_ProcessLayerPerHareOutput_emptyOutput(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	_, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gPlus1)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr, gLyr, false).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), hareOutput)
	require.Equal(t, gPlus1, tm.ProcessedLayer())

	gPlus2 := gLyr.Add(2)
	createLayerBallotsAndBlocks(t, tm.Mesh, gPlus2)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gLyr, gPlus1, false).Times(1)
	tm.mockState.EXPECT().ApplyLayer(gPlus1, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Return(nil).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, types.EmptyBlockID))

	// hare output saved
	hareOutput, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, types.EmptyBlockID, hareOutput)

	// but processed layer has advanced
	assert.Equal(t, gPlus2, tm.ProcessedLayer())
}

func TestMesh_PersistProcessedLayer(t *testing.T) {
	tm := createTestMesh(t)
	t.Cleanup(func() {
		tm.Close()
	})
	layerID := types.NewLayerID(7)
	assert.NoError(t, tm.persistProcessedLayer(layerID))
	rLyr, err := tm.GetProcessedLayer()
	assert.NoError(t, err)
	assert.Equal(t, layerID, rLyr)
}

func TestMesh_LatestKnownLayer(t *testing.T) {
	tm := createTestMesh(t)
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
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	ballots1, blocks1 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(1))
	ballots2, blocks2 := createLayerBallotsAndBlocks(t, tm.Mesh, gLyr.Add(2))

	for _, b := range append(ballots1, ballots2...) {
		got, err := tm.GetBallot(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
	for _, b := range append(blocks1, blocks2...) {
		got, err := tm.GetBlock(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
	recoveredMesh := NewMesh(tm.DB, nil, tm.mockTortoise, tm.mockState, logtest.New(t))
	for _, b := range append(ballots1, ballots2...) {
		got, err := recoveredMesh.GetBallot(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
	for _, b := range append(blocks1, blocks2...) {
		got, err := recoveredMesh.GetBlock(b.ID())
		assert.NoError(t, err)
		assert.Equal(t, b, got)
	}
}

func TestMesh_pushLayersToState(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()
	tm.mockTortoise.EXPECT().OnBallot(gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(1)
	createLayerBallots(t, tm.Mesh, layerID)

	txIDs := types.RandomTXSet(5)
	pendingTXs := map[types.TransactionID]struct{}{}
	for _, id := range txIDs {
		pendingTXs[id] = struct{}{}
	}
	// either block1 or block2 will be applied to the state
	tm.mockState.EXPECT().StoreTransactionsFromMemPool(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	block1 := addBlockWithTXsToMesh(t, tm.Mesh, layerID, true, txIDs[:1])
	block2 := addBlockWithTXsToMesh(t, tm.Mesh, layerID, true, txIDs[1:4])
	addBlockWithTXsToMesh(t, tm.Mesh, layerID, false, txIDs[3:])
	addBlockWithTXsToMesh(t, tm.Mesh, layerID, false, txIDs[4:])

	valids := []*types.Block{block1, block2}
	types.SortBlocks(valids)
	hareOutput := valids[0]
	for _, txID := range hareOutput.TxIDs {
		delete(pendingTXs, txID)
	}
	var reinsert []types.TransactionID
	for _, id := range txIDs {
		if _, ok := pendingTXs[id]; ok {
			reinsert = append(reinsert, id)
		}
	}

	tm.mockState.EXPECT().ApplyLayer(layerID, hareOutput.ID(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ types.LayerID, _ types.BlockID, ids []types.TransactionID, _ map[types.Address]uint64) ([]*types.Transaction, error) {
			assert.ElementsMatch(t, hareOutput.TxIDs, ids)
			return nil, nil
		}).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any()).Do(
		func(got []types.TransactionID) error {
			assert.ElementsMatch(t, got, reinsert)
			return nil
		}).Times(1)
	require.NoError(t, tm.pushLayersToState(context.TODO(), types.GetEffectiveGenesis().Add(1), types.GetEffectiveGenesis().Add(1)))
}

func TestMesh_persistLayerHash(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	// persist once
	lyr := types.NewLayerID(3)
	hash := types.RandomHash()
	require.NoError(t, tm.persistLayerHash(lyr, hash))
	assert.Equal(t, hash, tm.GetLayerHash(lyr))

	// persist twice
	newHash := types.RandomHash()
	assert.NotEqual(t, newHash, hash)
	require.NoError(t, tm.persistLayerHash(lyr, newHash))
	assert.Equal(t, newHash, tm.GetLayerHash(lyr))
}

func addBlockWithTXsToMesh(t *testing.T, msh *Mesh, id types.LayerID, valid bool, txIDs []types.TransactionID) *types.Block {
	t.Helper()
	b := types.GenLayerBlock(id, txIDs)
	require.NoError(t, msh.AddBlockWithTXs(context.TODO(), b))
	require.NoError(t, msh.SaveContextualValidity(b.ID(), id, valid))
	return b
}

func TestMesh_AddTXsFromProposal(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	txIDs := types.RandomTXSet(numTXs)
	layerID := types.GetEffectiveGenesis().Add(1)
	tm.mockState.EXPECT().StoreTransactionsFromMemPool(layerID, types.EmptyBlockID, txIDs).Return(nil).Times(1)
	r.NoError(tm.AddTXsFromProposal(context.TODO(), layerID, types.RandomProposalID(), txIDs))
}

func TestMesh_AddBlockWithTXs(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()
	tm.mockTortoise.EXPECT().OnBallot(gomock.Any()).AnyTimes()

	txIDs := types.RandomTXSet(numTXs)
	layerID := types.GetEffectiveGenesis().Add(1)
	block := types.GenLayerBlock(layerID, txIDs)
	tm.mockState.EXPECT().StoreTransactionsFromMemPool(layerID, block.ID(), txIDs).Return(nil).Times(1)
	r.NoError(tm.AddBlockWithTXs(context.TODO(), block))
}

func TestMesh_ReverifyFailed(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(10)

	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(maxLayer(lid.Sub(2), genesis), lid.Sub(1), false)
		if lid.Sub(1).After(genesis) {
			tm.mockState.EXPECT().GetStateRoot()
		}
		tm.ProcessLayer(ctx, lid)
	}

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	last = last.Add(1)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(last.Sub(1), last, false)

	block := types.Block{}
	block.LayerIndex = last
	block.TxIDs = []types.TransactionID{{1, 1, 1}}
	require.NoError(t, tm.AddBlock(&block))
	require.NoError(t, tm.SaveContextualValidity(block.ID(), last, true))
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(block.LayerIndex, block.ID(), block.TxIDs, gomock.Any()).Return(nil, errTXMissing)
	tm.mockState.EXPECT().ReinsertTxsToMemPool(gomock.Any())
	tm.mockState.EXPECT().GetStateRoot()
	require.Error(t, tm.ProcessLayer(ctx, last))
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.MissingLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	last = last.Add(1)
	for lid := last.Sub(1); !lid.After(last); lid = lid.Add(1) {
		require.NoError(t, tm.SaveContextualValidity(block.ID(), last, false))
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(lid.Sub(1), last.Sub(1), false)
	}

	tm.mockState.EXPECT().GetStateRoot()
	for lid := last.Sub(1); !lid.After(last); lid = lid.Add(1) {
		tm.ProcessLayer(ctx, lid)
	}

	require.Empty(t, tm.MissingLayer())
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())
}

func TestMesh_MissingTransactionsFailure(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(1)

	block := types.Block{}
	block.LayerIndex = last
	block.TxIDs = []types.TransactionID{{1, 1, 1}}
	require.NoError(t, tm.AddBlock(&block))
	require.NoError(t, tm.SaveContextualValidity(block.ID(), last, true))

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(genesis, last, false)
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(block.LayerIndex, block.ID(), block.TxIDs, gomock.Any()).Return(nil, errTXMissing)
	require.ErrorIs(t, tm.ProcessLayer(ctx, last), errTXMissing)

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, genesis, tm.LatestLayerInState())
}

func TestMesh_ResetAppliedOnRevert(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(10)

	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(lid.Sub(1), lid, false)
		tm.mockState.EXPECT().GetStateRoot()
		tm.SetZeroBlockLayer(lid)
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}

	require.Equal(t, last.Sub(1), tm.ProcessedLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	failed := genesis.Add(2)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(failed.Sub(1), last, true)
	tm.mockState.EXPECT().Rewind(failed.Sub(1))

	block := types.Block{}
	block.LayerIndex = failed
	block.TxIDs = []types.TransactionID{{1, 1, 1}}
	require.NoError(t, tm.AddBlock(&block))
	require.NoError(t, tm.SaveContextualValidity(block.ID(), failed, true))
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(block.LayerIndex, block.ID(), block.TxIDs, gomock.Any()).Return(nil, errTXMissing)
	require.ErrorIs(t, tm.ProcessLayer(ctx, last), errTXMissing)

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, failed.Sub(1), tm.LatestLayerInState())
}

func TestMesh_NoPanicOnIncorrectVerified(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	const n = 10
	last := genesis.Add(n)

	tm.mockState.EXPECT().GetStateRoot().Times(n - 1)
	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(maxLayer(lid.Sub(2), genesis), lid.Sub(1), false)
		tm.ProcessLayer(ctx, lid)
	}
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(tm.LatestLayerInState().Sub(2), tm.LatestLayerInState().Sub(1), false)
	tm.ProcessLayer(ctx, last.Add(1))
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())

	tm.mockState.EXPECT().GetStateRoot().Times(2) // will apply two layers
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(last, last.Add(1), false)
	tm.ProcessLayer(ctx, last.Add(2))
	require.Equal(t, last.Add(1), tm.LatestLayerInState())
}

func TestMesh_CallOnBlock(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	block := types.Block{}
	block.LayerIndex = types.NewLayerID(10)
	block.Initialize()

	tm.mockTortoise.EXPECT().OnBlock(&block)
	tm.mockState.EXPECT().StoreTransactionsFromMemPool(block.LayerIndex, block.ID(), block.TxIDs).Times(1)
	require.NoError(t, tm.AddBlockWithTXs(context.TODO(), &block))
}

func TestMesh_CallOnBallot(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.ctrl.Finish()
	defer tm.Close()

	ballot := types.RandomBallot()

	tm.mockTortoise.EXPECT().OnBallot(ballot)

	require.NoError(t, tm.AddBallot(ballot))
}
