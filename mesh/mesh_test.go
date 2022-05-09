package mesh

import (
	"context"
	"errors"
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
	checkLastApplied(t, tm.Mesh, types.GetEffectiveGenesis())
	return tm
}

func createBlock(t testing.TB, mesh *Mesh, layerID types.LayerID, nodeID types.NodeID, valid bool) *types.Block {
	t.Helper()
	txIDs := types.RandomTXSet(numTXs)
	coinbase := types.BytesToAddress(nodeID[:])
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
	require.NoError(t, mesh.SaveContextualValidity(b.ID(), layerID, valid))
	return b
}

func createLayerBallotsAndBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID) ([]*types.Ballot, []*types.Block) {
	t.Helper()
	return createLayerBallots(t, mesh, lyrID), createLayerBlocks(t, mesh, lyrID, true)
}

func createLayerBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID, valid bool) []*types.Block {
	t.Helper()
	blocks := make([]*types.Block, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		nodeID := types.NodeID{byte(i)}
		blk := createBlock(t, mesh, lyrID, nodeID, valid)
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

func checkLastApplied(t *testing.T, mesh *Mesh, expected types.LayerID) {
	lid, err := mesh.LastAppliedLayer()
	require.NoError(t, err)
	require.Equal(t, expected, lid)
}

func TestMesh_LayerHashes(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	lyrBlocks := make(map[types.LayerID]*types.Block)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		blocks := createLayerBlocks(t, tm.Mesh, i, true)
		hareOutput := types.SortBlocks(blocks)[0]
		lyrBlocks[i] = hareOutput
		require.NoError(t, tm.SaveHareConsensusOutput(context.TODO(), i, hareOutput.ID()))
	}

	prevAggHash := tm.GetAggregatedLayerHash(gLyr)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		require.NoError(t, err)

		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(1)).Times(1)
		blk := lyrBlocks[i]
		tm.mockState.EXPECT().ApplyLayer(blk).Return(nil, nil).Times(1)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{})

		require.Equal(t, types.EmptyLayerHash, tm.GetLayerHash(i))
		require.Equal(t, types.EmptyLayerHash, tm.GetAggregatedLayerHash(i))
		require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
		expectedHash := types.CalcBlocksHash32([]types.BlockID{blk.ID()}, nil)
		assert.Equal(t, expectedHash, tm.GetLayerHash(i))
		expectedAggHash := types.CalcBlocksHash32([]types.BlockID{blk.ID()}, prevAggHash.Bytes())
		assert.Equal(t, expectedAggHash, tm.GetAggregatedLayerHash(i))
		prevAggHash = expectedAggHash
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

	blocks := createLayerBlocks(t, tm.Mesh, id, true)
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
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1, false)
	blocks2 := createLayerBlocks(t, tm.Mesh, gPlus2, false)
	blocks3 := createLayerBlocks(t, tm.Mesh, gPlus3, false)
	blocks4 := createLayerBlocks(t, tm.Mesh, gPlus4, false)
	blocks5 := createLayerBlocks(t, tm.Mesh, gPlus5, false)
	layerBlocks := map[types.LayerID]*types.Block{
		gPlus1: blocks1[0],
		gPlus2: blocks2[0],
		gPlus3: blocks3[0],
		gPlus4: blocks4[0],
		gPlus5: blocks5[0],
	}

	for i := gPlus1; !i.After(gPlus5); i = i.Add(1) {
		toApply := layerBlocks[i]
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(1)).Times(1)
		tm.mockState.EXPECT().ApplyLayer(toApply).Return(nil, nil).Times(1)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
		require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), i, toApply.ID()))
		got, err := tm.GetHareConsensusOutput(i)
		require.NoError(t, err)
		assert.Equal(t, toApply.ID(), got)
		assert.Equal(t, i, tm.ProcessedLayer())
	}
	checkLastApplied(t, tm.Mesh, gPlus5)
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
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1, true)
	blocks2 := createLayerBlocks(t, tm.Mesh, gPlus2, true)
	blocks3 := createLayerBlocks(t, tm.Mesh, gPlus3, true)
	blocks4 := createLayerBlocks(t, tm.Mesh, gPlus4, true)
	blocks5 := createLayerBlocks(t, tm.Mesh, gPlus5, true)

	// process order is  : gPlus1, gPlus3, gPlus5, gPlus2, gPlus4
	// processed layer is: gPlus1, gPlus1, gPlus1, gPlus3, gPlus5
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr).Times(1)
	tm.mockState.EXPECT().ApplyLayer(blocks1[0]).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	got, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	assert.Equal(t, blocks1[0].ID(), got)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())
	assert.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus1)

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus3).Return(gLyr).Times(1)
	// will try to apply state for gPlus2
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus3, blocks3[0].ID())
	assert.ErrorIs(t, err, errMissingHareOutput)
	got, err = tm.GetHareConsensusOutput(gPlus3)
	require.NoError(t, err)
	assert.Equal(t, blocks3[0].ID(), got)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())
	assert.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus1)

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gLyr).Times(1)
	// will try to apply state for gPlus2
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID())
	assert.ErrorIs(t, err, errMissingHareOutput)
	got, err = tm.GetHareConsensusOutput(gPlus5)
	require.NoError(t, err)
	assert.Equal(t, blocks5[0].ID(), got)
	assert.Equal(t, gPlus1, tm.ProcessedLayer())
	assert.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus1)

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gPlus2).Times(1)
	// will try to apply state for gPlus2, gPlus3 and gPlus4
	// since gPlus2 has been verified, we will apply the lowest order of contextually valid blocks
	gPlus2Block := types.SortBlocks(blocks2)[0]
	tm.mockState.EXPECT().ApplyLayer(gPlus2Block).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().ApplyLayer(blocks3[0]).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(2)
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blocks2[0].ID())
	assert.ErrorIs(t, err, errMissingHareOutput)
	got, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, blocks2[0].ID(), got)
	assert.Equal(t, gPlus3, tm.ProcessedLayer())
	assert.Equal(t, gPlus3, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus3)

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus4).Times(1)
	// will try to apply state for gPlus4 and gPlus5
	// since gPlus4 has been verified, we will apply the lowest order of contextually valid blocks
	gPlus4Block := types.SortBlocks(blocks4)[0]
	tm.mockState.EXPECT().ApplyLayer(gPlus4Block).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().ApplyLayer(blocks5[0]).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(2)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	got, err = tm.GetHareConsensusOutput(gPlus4)
	require.NoError(t, err)
	assert.Equal(t, blocks4[0].ID(), got)
	assert.Equal(t, gPlus5, tm.ProcessedLayer())
	assert.Equal(t, gPlus5, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus5)
}

func TestMesh_ProcessLayerPerHareOutput_emptyOutput(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1, false)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr).Times(1)
	tm.mockState.EXPECT().ApplyLayer(blocks1[0]).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := tm.GetHareConsensusOutput(gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), hareOutput)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	checkLastApplied(t, tm.Mesh, gPlus1)

	gPlus2 := gLyr.Add(2)
	createLayerBlocks(t, tm.Mesh, gPlus2, false)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gPlus1).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, types.EmptyBlockID))

	// hare output saved
	hareOutput, err = tm.GetHareConsensusOutput(gPlus2)
	require.NoError(t, err)
	assert.Equal(t, types.EmptyBlockID, hareOutput)

	// but processed layer has advanced
	assert.Equal(t, gPlus2, tm.ProcessedLayer())
	assert.Equal(t, gPlus2, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus2)
}

func TestMesh_Revert(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	blocks1 := createLayerBlocks(t, tm.Mesh, gLyr.Add(1), false)
	blocks2 := createLayerBlocks(t, tm.Mesh, gLyr.Add(2), false)
	blocks3 := createLayerBlocks(t, tm.Mesh, gLyr.Add(3), false)
	blocks4 := createLayerBlocks(t, tm.Mesh, gLyr.Add(4), false)
	blocks5 := createLayerBlocks(t, tm.Mesh, gLyr.Add(5), false)
	layerBlocks := map[types.LayerID]*types.Block{
		gPlus1: blocks1[0],
		gPlus2: blocks2[0],
		gPlus3: blocks3[0],
		gPlus4: blocks4[0],
		gPlus5: blocks5[0],
	}

	for i := gPlus1; i.Before(gPlus4); i = i.Add(1) {
		hareOutput := layerBlocks[i]
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(1)).Times(1)
		tm.mockState.EXPECT().ApplyLayer(hareOutput).Return(nil, nil).Times(1)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
		require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), i, hareOutput.ID()))
	}
	require.Equal(t, gPlus3, tm.ProcessedLayer())
	require.Equal(t, gPlus3, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus3)

	oldHash := tm.GetAggregatedLayerHash(gPlus2)
	require.NotEqual(t, types.EmptyLayerHash, oldHash)

	// for layer gPlus2, every other block turns out to be valid
	layerBlocks[gPlus2] = types.SortBlocks(blocks2[1:])[0]
	for _, blk := range blocks2[1:] {
		require.NoError(t, tm.UpdateBlockValidity(blk.ID(), gPlus2, true))
	}
	for lyr, blk := range layerBlocks {
		require.NoError(t, tm.UpdateBlockValidity(blk.ID(), lyr, true))
	}

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus3).Times(1)
	tm.mockState.EXPECT().RevertState(gPlus1).Return(types.Hash32{}, nil)
	for i := gPlus2; !i.After(gPlus4); i = i.Add(1) {
		hareOutput := layerBlocks[i]
		tm.mockState.EXPECT().ApplyLayer(hareOutput).Return(nil, nil).Times(1)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	}
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	require.Equal(t, gPlus4, tm.ProcessedLayer())
	require.Equal(t, gPlus4, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus4)
	newHash := tm.GetAggregatedLayerHash(gPlus2)
	require.NotEqual(t, types.EmptyLayerHash, newHash)
	require.NotEqual(t, oldHash, newHash)

	// gPlus2 hash should contain all valid blocks
	require.Equal(t, types.CalcBlocksHash32(types.ToBlockIDs(types.SortBlocks(blocks2[1:])), nil), tm.GetLayerHash(gPlus2))

	// another new layer won't cause a revert
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gPlus4).Times(1)
	tm.mockState.EXPECT().ApplyLayer(blocks5[0]).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID()))
	require.Equal(t, gPlus5, tm.ProcessedLayer())
	require.Equal(t, gPlus5, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, gPlus5)
	require.Equal(t, newHash, tm.GetAggregatedLayerHash(gPlus2))
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

func TestMesh_pushLayersToState_verified(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()
	tm.mockTortoise.EXPECT().OnBallot(gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(1)
	createLayerBallots(t, tm.Mesh, layerID)

	txIDs := types.RandomTXSet(5)
	block1 := addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[:1])
	block2 := addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[1:4])
	block3 := addBlockWithTXsToMesh(t, tm, layerID, false, txIDs[3:])
	addBlockWithTXsToMesh(t, tm, layerID, false, txIDs[4:])

	// set block3 to be hare output
	require.NoError(t, tm.SaveHareConsensusOutput(context.TODO(), layerID, block3.ID()))

	valids := []*types.Block{block1, block2}
	types.SortBlocks(valids)
	toApply := valids[0]
	tm.mockState.EXPECT().ApplyLayer(toApply).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	require.NoError(t, tm.pushLayersToState(context.TODO(), layerID, layerID, layerID))
	checkLastApplied(t, tm.Mesh, layerID)
}

func TestMesh_pushLayersToState_notVerified(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()
	tm.mockTortoise.EXPECT().OnBallot(gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(1)
	createLayerBallots(t, tm.Mesh, layerID)

	txIDs := types.RandomTXSet(5)
	// either block1 or block2 will be applied to the state
	addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[:1])
	addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[1:4])
	addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[3:])
	hareOutput := addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[4:])
	require.NoError(t, tm.SaveHareConsensusOutput(context.TODO(), layerID, hareOutput.ID()))

	tm.mockState.EXPECT().ApplyLayer(hareOutput).Return(nil, nil).Times(1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}).Times(1)
	require.NoError(t, tm.pushLayersToState(context.TODO(), layerID, layerID, layerID.Sub(1)))
	checkLastApplied(t, tm.Mesh, layerID)
}

func TestMesh_persistLayerHash(t *testing.T) {
	tm := createTestMesh(t)
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

func addBlockWithTXsToMesh(t *testing.T, tm *testMesh, id types.LayerID, valid bool, txIDs []types.TransactionID) *types.Block {
	t.Helper()
	b := types.GenLayerBlock(id, txIDs)
	tm.mockState.EXPECT().LinkTXsWithBlock(id, b.ID(), b.TxIDs).Times(1)
	require.NoError(t, tm.Mesh.AddBlockWithTXs(context.TODO(), b))
	require.NoError(t, tm.Mesh.SaveContextualValidity(b.ID(), id, valid))
	return b
}

func TestMesh_AddTXsFromProposal(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	defer tm.Close()

	layerID := types.GetEffectiveGenesis().Add(1)
	pid := types.RandomProposalID()
	txIDs := types.RandomTXSet(numTXs)
	tm.mockState.EXPECT().LinkTXsWithProposal(layerID, pid, txIDs).Return(nil).Times(1)
	r.NoError(tm.AddTXsFromProposal(context.TODO(), layerID, pid, txIDs))
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
	tm.mockState.EXPECT().LinkTXsWithBlock(layerID, block.ID(), txIDs).Return(nil).Times(1)
	r.NoError(tm.AddBlockWithTXs(context.TODO(), block))
}

func TestMesh_ReverifyFailed(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(10)

	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		require.NoError(t, tm.SaveHareConsensusOutput(ctx, lid, types.EmptyBlockID))
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, last)

	last = last.Add(1)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last).Return(last.Sub(1))

	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{LayerIndex: last, TxIDs: []types.TransactionID{{1, 1, 1}}})
	require.NoError(t, tm.AddBlock(block))
	require.NoError(t, tm.SaveHareConsensusOutput(ctx, last, block.ID()))
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(block).Return(nil, errTXMissing)
	require.Error(t, tm.ProcessLayer(ctx, last))
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.MissingLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, last.Sub(1))

	last = last.Add(1)
	require.NoError(t, tm.SaveContextualValidity(block.ID(), last.Sub(1), true))
	tm.mockState.EXPECT().ApplyLayer(block).Return(nil, nil)
	require.NoError(t, tm.SaveHareConsensusOutput(ctx, last, types.EmptyBlockID))
	for lid := last.Sub(1); !lid.After(last); lid = lid.Add(1) {
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(last.Sub(1))
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}

	require.Empty(t, tm.MissingLayer())
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, last)
}

func TestMesh_MissingTransactionsFailure(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(1)

	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{LayerIndex: last, TxIDs: []types.TransactionID{{1, 1, 1}}})
	require.NoError(t, tm.AddBlock(block))
	require.NoError(t, tm.SaveHareConsensusOutput(ctx, last, block.ID()))

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last).Return(last.Sub(1))
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(block).Return(nil, errTXMissing)
	require.ErrorIs(t, tm.ProcessLayer(ctx, last), errTXMissing)

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, genesis, tm.LatestLayerInState())
	checkLastApplied(t, tm.Mesh, genesis)
}

func TestMesh_NoPanicOnIncorrectVerified(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	const n = 10
	last := genesis.Add(n)

	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		require.NoError(t, tm.SaveHareConsensusOutput(ctx, lid, types.EmptyBlockID))
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}
	require.Equal(t, last, tm.LatestLayerInState())

	require.NoError(t, tm.SaveHareConsensusOutput(ctx, last.Add(1), types.EmptyBlockID))
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last.Add(1)).Return(tm.LatestLayerInState().Sub(1))
	tm.mockState.EXPECT().GetStateRoot()
	require.NoError(t, tm.ProcessLayer(ctx, last.Add(1)))
	require.Equal(t, last.Add(1), tm.LatestLayerInState())

	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last.Add(2)).Return(last.Add(1))
	require.ErrorIs(t, tm.ProcessLayer(ctx, last.Add(2)), errMissingHareOutput)
	require.Equal(t, last.Add(1), tm.LatestLayerInState())
}

func TestMesh_CallOnBlock(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	block := types.Block{}
	block.LayerIndex = types.NewLayerID(10)
	block.Initialize()

	tm.mockTortoise.EXPECT().OnBlock(&block)
	tm.mockState.EXPECT().LinkTXsWithBlock(block.LayerIndex, block.ID(), block.TxIDs).Times(1)
	require.NoError(t, tm.AddBlockWithTXs(context.TODO(), &block))
}

func TestMesh_CallOnBallot(t *testing.T) {
	tm := createTestMesh(t)
	defer tm.Close()

	ballot := types.RandomBallot()

	tm.mockTortoise.EXPECT().OnBallot(ballot)

	require.NoError(t, tm.AddBallot(ballot))
}
