package mesh

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoise/opinionhash"
)

const (
	numBallots = 10
	numBlocks  = 5
	numTXs     = 20
)

type testMesh struct {
	*Mesh
	mockState    *mocks.MockconservativeState
	mockTortoise *smocks.MockTortoise
}

func saveContextualValidity(t *testing.T, db sql.Executor, bid types.BlockID, valid bool) {
	if valid {
		require.NoError(t, blocks.SetValid(db, bid))
	} else {
		require.NoError(t, blocks.SetInvalid(db, bid))
	}
}

func createTestMesh(t *testing.T) *testMesh {
	t.Helper()
	types.SetLayersPerEpoch(3)
	lg := logtest.New(t)
	ctrl := gomock.NewController(t)
	tm := &testMesh{
		mockState:    mocks.NewMockconservativeState(ctrl),
		mockTortoise: smocks.NewMockTortoise(ctrl),
	}
	msh, err := NewMesh(datastore.NewCachedDB(sql.InMemory(), lg), tm.mockTortoise, tm.mockState, lg)
	require.NoError(t, err)
	gLid := types.GetEffectiveGenesis()
	checkLastAppliedInDB(t, msh, gLid)
	checkProcessedInDB(t, msh, gLid)
	tm.Mesh = msh
	return tm
}

func createBlock(t testing.TB, mesh *Mesh, layerID types.LayerID, nodeID types.NodeID) *types.Block {
	t.Helper()
	txIDs := types.RandomTXSet(numTXs)
	weight := util.WeightFromFloat64(312.13)
	b := &types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: layerID,
			Rewards: []types.AnyReward{
				{
					Coinbase: types.GenerateAddress(nodeID[:]),
					Weight:   types.RatNum{Num: weight.Num().Uint64(), Denom: weight.Denom().Uint64()},
				},
			},
			TxIDs: txIDs,
		},
	}
	b.Initialize()
	require.NoError(t, blocks.Add(mesh.cdb, b))
	return b
}

func createLayerBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID) []*types.Block {
	t.Helper()
	blks := make([]*types.Block, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		nodeID := types.NodeID{byte(i)}
		blk := createBlock(t, mesh, lyrID, nodeID)
		blks = append(blks, blk)
	}
	return blks
}

func createLayerBallots(t *testing.T, mesh *Mesh, lyrID types.LayerID) []*types.Ballot {
	t.Helper()
	blts := make([]*types.Ballot, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballot := types.GenLayerBallot(lyrID)
		blts = append(blts, ballot)
		require.NoError(t, mesh.AddBallot(context.TODO(), ballot))
	}
	return blts
}

func checkLastAppliedInDB(t *testing.T, mesh *Mesh, expected types.LayerID) {
	t.Helper()
	lid, err := layers.GetLastApplied(mesh.cdb)
	require.NoError(t, err)
	require.Equal(t, expected, lid)
}

func checkProcessedInDB(t *testing.T, mesh *Mesh, expected types.LayerID) {
	t.Helper()
	lid, err := layers.GetProcessed(mesh.cdb)
	require.NoError(t, err)
	require.Equal(t, expected, lid)
}

func TestMesh_FromGenesis(t *testing.T) {
	tm := createTestMesh(t)
	gotP := tm.Mesh.ProcessedLayer()
	require.Equal(t, types.GetEffectiveGenesis(), gotP)
	getLS := tm.Mesh.LatestLayerInState()
	require.Equal(t, types.GetEffectiveGenesis(), getLS)
	gotL := tm.Mesh.LatestLayer()
	require.Equal(t, types.GetEffectiveGenesis(), gotL)

	opinion, err := layers.GetAggregatedHash(tm.cdb, types.GetEffectiveGenesis())
	require.NoError(t, err)
	buf := hash.Sum(nil)
	require.Equal(t, buf[:], opinion[:])
}

func TestMesh_WakeUpWhileGenesis(t *testing.T) {
	tm := createTestMesh(t)
	msh, err := NewMesh(tm.cdb, tm.mockTortoise, tm.mockState, logtest.New(t))
	require.NoError(t, err)
	gLid := types.GetEffectiveGenesis()
	checkProcessedInDB(t, msh, gLid)
	checkLastAppliedInDB(t, msh, gLid)
	gotL := msh.LatestLayer()
	require.Equal(t, gLid, gotL)
	gotP := msh.ProcessedLayer()
	require.Equal(t, gLid, gotP)
	gotLS := msh.LatestLayerInState()
	require.Equal(t, gLid, gotLS)
}

func TestMesh_WakeUp(t *testing.T) {
	tm := createTestMesh(t)
	latest := types.NewLayerID(11)
	b := types.NewExistingBallot(types.BallotID{1, 2, 3}, []byte{}, []byte{}, types.InnerBallot{LayerIndex: latest})
	require.NoError(t, ballots.Add(tm.cdb, &b))
	require.NoError(t, layers.SetProcessed(tm.cdb, latest))
	latestState := latest.Sub(1)
	require.NoError(t, layers.SetApplied(tm.cdb, latestState, types.RandomBlockID()))

	tm.mockState.EXPECT().RevertState(latestState).Return(nil)
	msh, err := NewMesh(tm.cdb, tm.mockTortoise, tm.mockState, logtest.New(t))
	require.NoError(t, err)
	gotL := msh.LatestLayer()
	require.Equal(t, latest, gotL)
	gotP := msh.ProcessedLayer()
	require.Equal(t, latest, gotP)
	gotLS := msh.LatestLayerInState()
	require.Equal(t, latestState, gotLS)
}

func TestMesh_LayerHashes(t *testing.T) {
	tm := createTestMesh(t)
	gLyr := types.GetEffectiveGenesis()
	numLayers := 5
	latestLyr := gLyr.Add(uint32(numLayers))
	lyrBlocks := make(map[types.LayerID]*types.Block)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		blks := createLayerBlocks(t, tm.Mesh, i)
		hareOutput := sortBlocks(blks)[0]
		lyrBlocks[i] = hareOutput
		require.NoError(t, certificates.SetHareOutput(tm.cdb, i, hareOutput.ID()))
	}

	prevAggHash, err := layers.GetAggregatedHash(tm.cdb, gLyr)
	require.NoError(t, err)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		require.NoError(t, err)

		tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), i)
		tm.mockTortoise.EXPECT().Updates().Return(i.Sub(1), nil)
		blk := lyrBlocks[i]
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), i, blk).Return(nil)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)

		_, err = layers.GetHash(tm.cdb, i)
		require.ErrorIs(t, err, sql.ErrNotFound)
		_, err = layers.GetAggregatedHash(tm.cdb, i)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
		expectedHash := types.CalcBlocksHash32([]types.BlockID{blk.ID()}, nil)
		h, err := layers.GetHash(tm.cdb, i)
		require.NoError(t, err)
		require.Equal(t, expectedHash, h)
		hasher := opinionhash.New()
		hasher.WritePrevious(prevAggHash)
		hasher.WriteSupport(blk.ID(), blk.TickHeight)
		expectedAggHash := hasher.Hash()
		ah, err := layers.GetAggregatedHash(tm.cdb, i)
		require.NoError(t, err)
		require.Equal(t, expectedAggHash, ah)
		prevAggHash = expectedAggHash
	}
}

func TestMesh_GetLayer(t *testing.T) {
	tm := createTestMesh(t)
	id := types.GetEffectiveGenesis().Add(1)
	lyr, err := tm.GetLayer(id)
	require.NoError(t, err)
	require.Empty(t, lyr.Blocks())
	require.Empty(t, lyr.Ballots())

	blks := createLayerBlocks(t, tm.Mesh, id)
	blts := createLayerBallots(t, tm.Mesh, id)
	lyr, err = tm.GetLayer(id)
	require.NoError(t, err)
	require.Equal(t, id, lyr.Index())
	require.Len(t, lyr.Ballots(), len(blts))
	require.ElementsMatch(t, blts, lyr.Ballots())
	require.ElementsMatch(t, blks, lyr.Blocks())
}

func TestMesh_ProcessLayerPerHareOutput(t *testing.T) {
	tm := createTestMesh(t)
	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1)
	blocks2 := createLayerBlocks(t, tm.Mesh, gPlus2)
	blocks3 := createLayerBlocks(t, tm.Mesh, gPlus3)
	blocks4 := createLayerBlocks(t, tm.Mesh, gPlus4)
	blocks5 := createLayerBlocks(t, tm.Mesh, gPlus5)
	layerBlocks := map[types.LayerID]*types.Block{
		gPlus1: blocks1[0],
		gPlus2: blocks2[0],
		gPlus3: blocks3[0],
		gPlus4: blocks4[0],
		gPlus5: blocks5[0],
	}

	for i := gPlus1; !i.After(gPlus5); i = i.Add(1) {
		toApply := layerBlocks[i]
		tm.mockTortoise.EXPECT().OnHareOutput(toApply.LayerIndex, toApply.ID())
		tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), i)
		tm.mockTortoise.EXPECT().Updates().Return(i.Sub(1), nil)
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), i, toApply).Return(nil)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
		require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), i, toApply.ID()))
		got, err := certificates.GetHareOutput(tm.cdb, i)
		require.NoError(t, err)
		require.Equal(t, toApply.ID(), got)
		require.Equal(t, i, tm.ProcessedLayer())
	}
	checkLastAppliedInDB(t, tm.Mesh, gPlus5)
}

func TestMesh_ProcessLayerPerHareOutput_OutOfOrder(t *testing.T) {
	tm := createTestMesh(t)
	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1)
	blocks2 := createLayerBlocks(t, tm.Mesh, gPlus2)
	blocks3 := createLayerBlocks(t, tm.Mesh, gPlus3)
	blocks4 := createLayerBlocks(t, tm.Mesh, gPlus4)
	blocks5 := createLayerBlocks(t, tm.Mesh, gPlus5)

	// process order is  : gPlus1, gPlus3, gPlus5, gPlus2, gPlus4
	// processed layer is: gPlus1, gPlus1, gPlus1, gPlus3, gPlus5
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus1)
	tm.mockTortoise.EXPECT().Updates().Return(gLyr, nil)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus1, blocks1[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus1, blocks1[0].ID())
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	got, err := certificates.GetHareOutput(tm.cdb, gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), got)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	require.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus3)
	tm.mockTortoise.EXPECT().Updates().Return(gLyr, nil)
	// will try to apply state for gPlus2
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus3, blocks3[0].ID())
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus3, blocks3[0].ID())
	require.ErrorIs(t, err, errMissingHareOutput)
	got, err = certificates.GetHareOutput(tm.cdb, gPlus3)
	require.NoError(t, err)
	require.Equal(t, blocks3[0].ID(), got)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	require.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus5)
	tm.mockTortoise.EXPECT().Updates().Return(gLyr, nil)
	// will try to apply state for gPlus2
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus5, blocks5[0].ID())
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID())
	require.ErrorIs(t, err, errMissingHareOutput)
	got, err = certificates.GetHareOutput(tm.cdb, gPlus5)
	require.NoError(t, err)
	require.Equal(t, blocks5[0].ID(), got)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	require.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus2)
	updates := makeValidityUpdates(gPlus1, []*types.Block{blocks1[0]}, blocks1[1:])
	updates = append(updates, makeValidityUpdates(gPlus2, blocks2, nil)...)
	tm.mockTortoise.EXPECT().Updates().Return(gPlus2, updates)
	// will try to apply state for gPlus2, gPlus3 and gPlus4
	// since gPlus2 has been verified, we will apply the lowest order of contextually valid blocks
	gPlus2Block := sortBlocks(blocks2)[0]
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus2, gPlus2Block).Return(nil)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus3, blocks3[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil).Times(2)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus2, blocks2[0].ID())
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blocks2[0].ID())
	require.ErrorIs(t, err, errMissingHareOutput)
	got, err = certificates.GetHareOutput(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.Equal(t, blocks2[0].ID(), got)
	require.Equal(t, gPlus3, tm.ProcessedLayer())
	require.Equal(t, gPlus3, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus3)

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus4)
	updates = makeValidityUpdates(gPlus3, []*types.Block{blocks3[0]}, blocks3[1:])
	updates = append(updates, makeValidityUpdates(gPlus4, blocks4, nil)...)
	tm.mockTortoise.EXPECT().Updates().Return(gPlus4, updates)
	// will try to apply state for gPlus4 and gPlus5
	// since gPlus4 has been verified, we will apply the lowest order of contextually valid blocks
	gPlus4Block := sortBlocks(blocks4)[0]
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus4, gPlus4Block).Return(nil)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus5, blocks5[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil).Times(2)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus4, blocks4[0].ID())
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	got, err = certificates.GetHareOutput(tm.cdb, gPlus4)
	require.NoError(t, err)
	require.Equal(t, blocks4[0].ID(), got)
	require.Equal(t, gPlus5, tm.ProcessedLayer())
	require.Equal(t, gPlus5, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus5)
}

func TestMesh_ProcessLayerPerHareOutput_emptyOutput(t *testing.T) {
	tm := createTestMesh(t)
	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus1, blocks1[0].ID())
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus1)
	tm.mockTortoise.EXPECT().Updates().Return(gLyr, nil)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus1, blocks1[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := certificates.GetHareOutput(tm.cdb, gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), hareOutput)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	gPlus2 := gLyr.Add(2)
	createLayerBlocks(t, tm.Mesh, gPlus2)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus2, types.EmptyBlockID)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus2, nil).Return(nil)
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus2)
	tm.mockTortoise.EXPECT().Updates().Return(gPlus1, nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, types.EmptyBlockID))

	// hare output saved
	hareOutput, err = certificates.GetHareOutput(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, hareOutput)

	// but processed layer has advanced
	require.Equal(t, gPlus2, tm.ProcessedLayer())
	require.Equal(t, gPlus2, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus2)
}

func makeValidityUpdates(lid types.LayerID, v, iv []*types.Block) []types.BlockContextualValidity {
	var res []types.BlockContextualValidity
	for _, b := range v {
		res = append(res, types.BlockContextualValidity{
			ID:       b.ID(),
			Layer:    lid,
			Validity: true,
		})
	}
	for _, b := range iv {
		res = append(res, types.BlockContextualValidity{
			ID:       b.ID(),
			Layer:    lid,
			Validity: false,
		})
	}
	return res
}

func TestMesh_Revert(t *testing.T) {
	tm := createTestMesh(t)
	gLyr := types.GetEffectiveGenesis()
	gPlus1 := gLyr.Add(1)
	gPlus2 := gLyr.Add(2)
	gPlus3 := gLyr.Add(3)
	gPlus4 := gLyr.Add(4)
	gPlus5 := gLyr.Add(5)
	blocks1 := createLayerBlocks(t, tm.Mesh, gLyr.Add(1))
	blocks2 := createLayerBlocks(t, tm.Mesh, gLyr.Add(2))
	blocks3 := createLayerBlocks(t, tm.Mesh, gLyr.Add(3))
	blocks4 := createLayerBlocks(t, tm.Mesh, gLyr.Add(4))
	blocks5 := createLayerBlocks(t, tm.Mesh, gLyr.Add(5))
	layerBlocks := map[types.LayerID]*types.Block{
		gPlus1: blocks1[0],
		gPlus2: blocks2[0],
		gPlus3: blocks3[0],
		gPlus4: blocks4[0],
		gPlus5: blocks5[0],
	}
	lyrUpdates := make(map[types.LayerID][]types.BlockContextualValidity)
	lyrUpdates[gPlus1] = makeValidityUpdates(gPlus1, []*types.Block{blocks1[0]}, blocks1[1:])
	lyrUpdates[gPlus2] = makeValidityUpdates(gPlus2, []*types.Block{blocks2[0]}, blocks2[1:])
	lyrUpdates[gPlus3] = makeValidityUpdates(gPlus3, []*types.Block{blocks3[0]}, blocks3[1:])
	lyrUpdates[gPlus4] = makeValidityUpdates(gPlus4, []*types.Block{blocks4[0]}, blocks4[1:])

	for i := gPlus1; i.Before(gPlus4); i = i.Add(1) {
		applied := layerBlocks[i]
		tm.mockTortoise.EXPECT().OnHareOutput(i, applied.ID())
		tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), i)
		updates := lyrUpdates[i.Sub(1)]
		tm.mockTortoise.EXPECT().Updates().Return(i.Sub(1), updates)
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), i, applied).Return(nil)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
		require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), i, applied.ID()))
	}
	require.Equal(t, gPlus3, tm.ProcessedLayer())
	require.Equal(t, gPlus3, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus3)

	oldHash, err := layers.GetAggregatedHash(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyLayerHash, oldHash)

	// for layer gPlus2 and gPlus3, all other block turns out to be valid
	layerBlocks[gPlus2] = sortBlocks(blocks2[1:])[0]
	layerBlocks[gPlus3] = sortBlocks(blocks3[1:])[0]
	newUpdates := makeValidityUpdates(gPlus2, blocks2[1:], blocks2[0:1])
	newUpdates = append(newUpdates, makeValidityUpdates(gPlus3, blocks3[1:], blocks3[0:1])...)
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus4, blocks4[0].ID())
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus4)
	tm.mockTortoise.EXPECT().Updates().Return(gPlus3, newUpdates)
	tm.mockState.EXPECT().RevertState(gPlus1).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	for i := gPlus2; !i.After(gPlus4); i = i.Add(1) {
		applied := layerBlocks[i]
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), i, applied).Return(nil)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	}
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	require.Equal(t, gPlus4, tm.ProcessedLayer())
	require.Equal(t, gPlus4, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus4)

	newHash, err := layers.GetAggregatedHash(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyLayerHash, newHash)
	require.NotEqual(t, oldHash, newHash)

	// gPlus2 hash should contain all valid blocks
	h, err := layers.GetHash(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.Equal(t, types.CalcBlocksHash32(types.ToBlockIDs(sortBlocks(blocks2[1:])), nil), h)

	// another new layer won't cause a revert
	tm.mockTortoise.EXPECT().OnHareOutput(gPlus5, blocks5[0].ID())
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), gPlus5)
	tm.mockTortoise.EXPECT().Updates().Return(gPlus4, lyrUpdates[gPlus4])
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus5, blocks5[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID()))
	require.Equal(t, gPlus5, tm.ProcessedLayer())
	require.Equal(t, gPlus5, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus5)
	ah, err := layers.GetAggregatedHash(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.Equal(t, newHash, ah)
}

func TestMesh_LatestKnownLayer(t *testing.T) {
	tm := createTestMesh(t)
	lg := logtest.New(t)
	tm.setLatestLayer(lg, types.NewLayerID(3))
	tm.setLatestLayer(lg, types.NewLayerID(7))
	tm.setLatestLayer(lg, types.NewLayerID(10))
	tm.setLatestLayer(lg, types.NewLayerID(1))
	tm.setLatestLayer(lg, types.NewLayerID(2))
	require.Equal(t, types.NewLayerID(10), tm.LatestLayer(), "wrong layer")
}

func TestMesh_pushLayersToState_verified(t *testing.T) {
	tm := createTestMesh(t)
	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(1)
	createLayerBallots(t, tm.Mesh, layerID)

	txIDs := types.RandomTXSet(5)
	block1 := addBlockWithTXsToMesh(t, tm, layerID, txIDs[:1])
	block2 := addBlockWithTXsToMesh(t, tm, layerID, txIDs[1:4])
	block3 := addBlockWithTXsToMesh(t, tm, layerID, txIDs[3:])
	addBlockWithTXsToMesh(t, tm, layerID, txIDs[4:])

	// set block3 to be hare output
	require.NoError(t, certificates.SetHareOutput(tm.cdb, layerID, block3.ID()))

	valids := []*types.Block{block1, block2}
	sortBlocks(valids)
	toApply := valids[0]
	for _, b := range valids {
		saveContextualValidity(t, tm.cdb, b.ID(), true)
	}
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), layerID, toApply).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.pushLayersToState(context.TODO(), tm.logger, layerID, layerID, layerID))
	checkLastAppliedInDB(t, tm.Mesh, layerID)
}

func TestMesh_ValidityOrder(t *testing.T) {
	for _, tc := range []struct {
		desc     string
		blocks   []*types.Block
		expected int
	}{
		{
			desc: "tick height",
			blocks: []*types.Block{
				types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{TickHeight: 100}),
				types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{TickHeight: 99}),
			},
			expected: 1,
		},
		{
			desc: "lexic",
			blocks: []*types.Block{
				types.NewExistingBlock(types.BlockID{2}, types.InnerBlock{TickHeight: 99}),
				types.NewExistingBlock(types.BlockID{1}, types.InnerBlock{TickHeight: 99}),
			},
			expected: 1,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			tm := createTestMesh(t)
			tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()
			tm.mockState.EXPECT().LinkTXsWithBlock(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

			lid := types.GetEffectiveGenesis().Add(1)
			for _, block := range tc.blocks {
				block.LayerIndex = lid
				require.NoError(t, tm.Mesh.AddBlockWithTXs(context.TODO(), block))
				saveContextualValidity(t, tm.cdb, block.ID(), true)
			}

			tm.mockState.EXPECT().ApplyLayer(context.TODO(), lid, tc.blocks[tc.expected]).Return(nil).Times(1)
			tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil).Times(1)
			require.NoError(t, tm.pushLayersToState(context.TODO(), tm.logger, lid, lid, lid))
		})
	}
}

func TestMesh_pushLayersToState_notVerified(t *testing.T) {
	tm := createTestMesh(t)
	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(1)
	createLayerBallots(t, tm.Mesh, layerID)

	txIDs := types.RandomTXSet(5)
	// either block1 or block2 will be applied to the state
	addBlockWithTXsToMesh(t, tm, layerID, txIDs[:1])
	addBlockWithTXsToMesh(t, tm, layerID, txIDs[1:4])
	addBlockWithTXsToMesh(t, tm, layerID, txIDs[3:])
	hareOutput := addBlockWithTXsToMesh(t, tm, layerID, txIDs[4:])
	require.NoError(t, certificates.SetHareOutput(tm.cdb, layerID, hareOutput.ID()))

	tm.mockState.EXPECT().ApplyLayer(context.TODO(), layerID, hareOutput).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.pushLayersToState(context.TODO(), tm.logger, layerID, layerID, layerID.Sub(1)))
	checkLastAppliedInDB(t, tm.Mesh, layerID)
}

func addBlockWithTXsToMesh(t *testing.T, tm *testMesh, id types.LayerID, txIDs []types.TransactionID) *types.Block {
	t.Helper()
	b := types.GenLayerBlock(id, txIDs)
	tm.mockState.EXPECT().LinkTXsWithBlock(id, b.ID(), b.TxIDs)
	require.NoError(t, tm.Mesh.AddBlockWithTXs(context.TODO(), b))
	return b
}

func TestMesh_AddTXsFromProposal(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	layerID := types.GetEffectiveGenesis().Add(1)
	pid := types.RandomProposalID()
	txIDs := types.RandomTXSet(numTXs)
	tm.mockState.EXPECT().LinkTXsWithProposal(layerID, pid, txIDs).Return(nil)
	r.NoError(tm.AddTXsFromProposal(context.TODO(), layerID, pid, txIDs))
}

func TestMesh_AddBlockWithTXs(t *testing.T) {
	r := require.New(t)
	tm := createTestMesh(t)
	tm.mockTortoise.EXPECT().OnBlock(gomock.Any()).AnyTimes()

	txIDs := types.RandomTXSet(numTXs)
	layerID := types.GetEffectiveGenesis().Add(1)
	block := types.GenLayerBlock(layerID, txIDs)
	tm.mockState.EXPECT().LinkTXsWithBlock(layerID, block.ID(), txIDs).Return(nil)
	r.NoError(tm.AddBlockWithTXs(context.TODO(), block))
}

func TestMesh_ReverifyFailed(t *testing.T) {
	tm := createTestMesh(t)
	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(10)

	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		require.NoError(t, certificates.SetHareOutput(tm.cdb, lid, types.EmptyBlockID))
		tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		tm.mockTortoise.EXPECT().Updates().Return(lid.Sub(1), nil)
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), lid, nil).Return(nil)
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, last)

	last = last.Add(1)
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), last)
	tm.mockTortoise.EXPECT().Updates().Return(last.Sub(1), nil)

	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{LayerIndex: last, TxIDs: []types.TransactionID{{1, 1, 1}}})
	require.NoError(t, blocks.Add(tm.cdb, block))
	require.NoError(t, certificates.SetHareOutput(tm.cdb, last, block.ID()))
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), last, block).Return(errTXMissing)
	require.Error(t, tm.ProcessLayer(ctx, last))
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.MissingLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, last.Sub(1))

	last = last.Add(1)
	saveContextualValidity(t, tm.cdb, block.ID(), true)
	require.NoError(t, certificates.SetHareOutput(tm.cdb, last, types.EmptyBlockID))
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), block.LayerIndex, block).Return(nil)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), last, nil).Return(nil)
	for lid := last.Sub(1); !lid.After(last); lid = lid.Add(1) {
		tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		tm.mockTortoise.EXPECT().Updates().Return(last.Sub(1), nil)
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}

	require.Empty(t, tm.MissingLayer())
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, last)
}

func TestMesh_MissingTransactionsFailure(t *testing.T) {
	tm := createTestMesh(t)
	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(1)

	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{LayerIndex: last, TxIDs: []types.TransactionID{{1, 1, 1}}})
	require.NoError(t, blocks.Add(tm.cdb, block))
	require.NoError(t, certificates.SetHareOutput(tm.cdb, last, block.ID()))

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), last)
	tm.mockTortoise.EXPECT().Updates().Return(last.Sub(1), nil)
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), last, block).Return(errTXMissing)
	require.ErrorIs(t, tm.ProcessLayer(ctx, last), errTXMissing)

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, genesis, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, genesis)
}

func TestMesh_NoPanicOnIncorrectVerified(t *testing.T) {
	tm := createTestMesh(t)
	ctx := context.TODO()
	genesis := types.GetEffectiveGenesis()
	const n = 10
	last := genesis.Add(n)

	for lid := genesis.Add(1); !lid.After(last); lid = lid.Add(1) {
		require.NoError(t, certificates.SetHareOutput(tm.cdb, lid, types.EmptyBlockID))
		tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		tm.mockTortoise.EXPECT().Updates().Return(lid.Sub(1), nil)
		tm.mockState.EXPECT().ApplyLayer(gomock.Any(), lid, nil)
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}
	require.Equal(t, last, tm.LatestLayerInState())

	require.NoError(t, certificates.SetHareOutput(tm.cdb, last.Add(1), types.EmptyBlockID))
	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), last.Add(1))
	tm.mockTortoise.EXPECT().Updates().Return(tm.LatestLayerInState().Sub(1), nil)
	tm.mockState.EXPECT().ApplyLayer(gomock.Any(), last.Add(1), nil)
	tm.mockState.EXPECT().GetStateRoot()
	require.NoError(t, tm.ProcessLayer(ctx, last.Add(1)))
	require.Equal(t, last.Add(1), tm.LatestLayerInState())

	tm.mockTortoise.EXPECT().TallyVotes(gomock.Any(), last.Add(2))
	tm.mockTortoise.EXPECT().Updates().Return(last.Add(1), nil)
	require.ErrorIs(t, tm.ProcessLayer(ctx, last.Add(2)), errMissingHareOutput)
	require.Equal(t, last.Add(1), tm.LatestLayerInState())
}

func TestMesh_CallOnBlock(t *testing.T) {
	tm := createTestMesh(t)
	block := types.Block{}
	block.LayerIndex = types.NewLayerID(10)
	block.Initialize()

	tm.mockTortoise.EXPECT().OnBlock(&block)
	tm.mockState.EXPECT().LinkTXsWithBlock(block.LayerIndex, block.ID(), block.TxIDs)
	require.NoError(t, tm.AddBlockWithTXs(context.TODO(), &block))
}

func TestMesh_MaliciousBallots(t *testing.T) {
	tm := createTestMesh(t)
	lid := types.NewLayerID(1)
	pub := []byte{1, 1, 1}

	blts := []types.Ballot{
		types.NewExistingBallot(types.BallotID{1}, nil, pub, types.InnerBallot{LayerIndex: lid}),
		types.NewExistingBallot(types.BallotID{2}, nil, pub, types.InnerBallot{LayerIndex: lid}),
		types.NewExistingBallot(types.BallotID{3}, nil, pub, types.InnerBallot{LayerIndex: lid}),
	}
	require.NoError(t, tm.AddBallot(context.TODO(), &blts[0]))
	require.False(t, blts[0].IsMalicious())
	for _, ballot := range blts[1:] {
		require.NoError(t, tm.AddBallot(context.TODO(), &ballot))
		require.True(t, ballot.IsMalicious())
	}
}
