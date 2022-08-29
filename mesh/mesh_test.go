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
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	numBallots = 10
	numBlocks  = 5
	numTXs     = 20
)

type testMesh struct {
	*Mesh
	mockCertHandler *mocks.MockcertHandler
	mockState       *mocks.MockconservativeState
	mockTortoise    *mocks.Mocktortoise
	mockBeacon      *smocks.MockBeaconGetter
}

func createTestMesh(t *testing.T) *testMesh {
	t.Helper()
	types.SetLayersPerEpoch(3)
	lg := logtest.New(t)
	ctrl := gomock.NewController(t)
	tm := &testMesh{
		mockCertHandler: mocks.NewMockcertHandler(ctrl),
		mockState:       mocks.NewMockconservativeState(ctrl),
		mockTortoise:    mocks.NewMocktortoise(ctrl),
		mockBeacon:      smocks.NewMockBeaconGetter(ctrl),
	}
	msh, err := NewMesh(datastore.NewCachedDB(sql.InMemory(), lg), tm.mockBeacon, tm.mockTortoise, tm.mockState, tm.mockCertHandler, lg)
	require.NoError(t, err)
	gLid := types.GetEffectiveGenesis()
	checkLastAppliedInDB(t, msh, gLid)
	checkLatestInDB(t, msh, gLid)
	checkProcessedInDB(t, msh, gLid)
	tm.Mesh = msh
	return tm
}

func createBlock(t testing.TB, mesh *Mesh, layerID types.LayerID, nodeID types.NodeID, valid bool) *types.Block {
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
	require.NoError(t, mesh.saveContextualValidity(b.ID(), layerID, valid))
	return b
}

func createLayerBlocks(t *testing.T, mesh *Mesh, lyrID types.LayerID, valid bool) []*types.Block {
	t.Helper()
	blks := make([]*types.Block, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		nodeID := types.NodeID{byte(i)}
		blk := createBlock(t, mesh, lyrID, nodeID, valid)
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
		require.NoError(t, mesh.addBallot(ballot))
	}
	return blts
}

func checkLastAppliedInDB(t *testing.T, mesh *Mesh, expected types.LayerID) {
	t.Helper()
	lid, err := layers.GetLastApplied(mesh.cdb)
	require.NoError(t, err)
	require.Equal(t, expected, lid)
}

func checkLatestInDB(t *testing.T, mesh *Mesh, expected types.LayerID) {
	t.Helper()
	lid, err := ballots.LatestLayer(mesh.cdb)
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

	gLayer := types.GenesisLayer()
	for _, b := range gLayer.Ballots() {
		has, err := ballots.Has(tm.cdb, b.ID())
		require.NoError(t, err)
		require.True(t, has)
	}

	for _, b := range gLayer.Ballots() {
		has, err := ballots.Has(tm.cdb, b.ID())
		require.NoError(t, err)
		require.True(t, has)
	}

	for _, b := range gLayer.Blocks() {
		has, err := blocks.Has(tm.cdb, b.ID())
		require.NoError(t, err)
		require.True(t, has)
		valid, err := blocks.IsValid(tm.cdb, b.ID())
		require.NoError(t, err)
		require.True(t, valid)
	}
	bid, err := layers.GetHareOutput(tm.cdb, gLayer.Index())
	require.NoError(t, err)
	require.Equal(t, types.GenesisBlockID, bid)
}

func TestMesh_WakeUpWhileGenesis(t *testing.T) {
	tm := createTestMesh(t)
	msh, err := NewMesh(tm.cdb, tm.mockBeacon, tm.mockTortoise, tm.mockState, tm.mockCertHandler, logtest.New(t))
	require.NoError(t, err)
	gLid := types.GetEffectiveGenesis()
	checkLatestInDB(t, msh, gLid)
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

	tm.mockState.EXPECT().RevertState(latestState).Return(types.RandomHash(), nil)
	msh, err := NewMesh(tm.cdb, tm.mockBeacon, tm.mockTortoise, tm.mockState, tm.mockCertHandler, logtest.New(t))
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
		blks := createLayerBlocks(t, tm.Mesh, i, true)
		hareOutput := sortBlocks(blks)[0]
		lyrBlocks[i] = hareOutput
		require.NoError(t, layers.SetHareOutput(tm.cdb, i, hareOutput.ID()))
	}

	prevAggHash, err := layers.GetAggregatedHash(tm.cdb, gLyr)
	require.NoError(t, err)
	for i := gLyr.Add(1); !i.After(latestLyr); i = i.Add(1) {
		thisLyr, err := tm.GetLayer(i)
		require.NoError(t, err)

		tm.mockBeacon.EXPECT().GetBeacon(i.GetEpoch()).Return(types.RandomBeacon(), nil)
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(1))
		blk := lyrBlocks[i]
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), blk).Return(nil)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)

		h, err := layers.GetHash(tm.cdb, i)
		require.NoError(t, err)
		require.Equal(t, types.EmptyLayerHash, h)
		ah, err := layers.GetAggregatedHash(tm.cdb, i)
		require.NoError(t, err)
		require.Equal(t, types.EmptyLayerHash, ah)
		require.NoError(t, tm.ProcessLayer(context.TODO(), thisLyr.Index()))
		expectedHash := types.CalcBlocksHash32([]types.BlockID{blk.ID()}, nil)
		h, err = layers.GetHash(tm.cdb, i)
		require.NoError(t, err)
		require.Equal(t, expectedHash, h)
		expectedAggHash := types.CalcBlocksHash32([]types.BlockID{blk.ID()}, prevAggHash.Bytes())
		ah, err = layers.GetAggregatedHash(tm.cdb, i)
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

	blks := createLayerBlocks(t, tm.Mesh, id, true)
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
		tm.mockBeacon.EXPECT().GetBeacon(i.GetEpoch()).Return(types.RandomBeacon(), nil)
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(1))
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), toApply).Return(nil)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
		require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), i, toApply.ID()))
		got, err := layers.GetHareOutput(tm.cdb, i)
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
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1, true)
	blocks2 := createLayerBlocks(t, tm.Mesh, gPlus2, true)
	blocks3 := createLayerBlocks(t, tm.Mesh, gPlus3, true)
	blocks4 := createLayerBlocks(t, tm.Mesh, gPlus4, true)
	blocks5 := createLayerBlocks(t, tm.Mesh, gPlus5, true)

	// process order is  : gPlus1, gPlus3, gPlus5, gPlus2, gPlus4
	// processed layer is: gPlus1, gPlus1, gPlus1, gPlus3, gPlus5
	tm.mockBeacon.EXPECT().GetBeacon(gPlus1.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), blocks1[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	got, err := layers.GetHareOutput(tm.cdb, gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), got)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	require.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	tm.mockBeacon.EXPECT().GetBeacon(gPlus3.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus3).Return(gLyr)
	// will try to apply state for gPlus2
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus3, blocks3[0].ID())
	require.ErrorIs(t, err, errMissingHareOutput)
	got, err = layers.GetHareOutput(tm.cdb, gPlus3)
	require.NoError(t, err)
	require.Equal(t, blocks3[0].ID(), got)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	require.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	tm.mockBeacon.EXPECT().GetBeacon(gPlus5.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gLyr)
	// will try to apply state for gPlus2
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus5, blocks5[0].ID())
	require.ErrorIs(t, err, errMissingHareOutput)
	got, err = layers.GetHareOutput(tm.cdb, gPlus5)
	require.NoError(t, err)
	require.Equal(t, blocks5[0].ID(), got)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	require.Equal(t, gPlus1, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	tm.mockBeacon.EXPECT().GetBeacon(gPlus2.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gPlus2)
	// will try to apply state for gPlus2, gPlus3 and gPlus4
	// since gPlus2 has been verified, we will apply the lowest order of contextually valid blocks
	gPlus2Block := sortBlocks(blocks2)[0]
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus2Block).Return(nil)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), blocks3[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil).Times(2)
	err = tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, blocks2[0].ID())
	require.ErrorIs(t, err, errMissingHareOutput)
	got, err = layers.GetHareOutput(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.Equal(t, blocks2[0].ID(), got)
	require.Equal(t, gPlus3, tm.ProcessedLayer())
	require.Equal(t, gPlus3, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus3)

	tm.mockBeacon.EXPECT().GetBeacon(gPlus4.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus4)
	// will try to apply state for gPlus4 and gPlus5
	// since gPlus4 has been verified, we will apply the lowest order of contextually valid blocks
	gPlus4Block := sortBlocks(blocks4)[0]
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), gPlus4Block).Return(nil)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), blocks5[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil).Times(2)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus4, blocks4[0].ID()))
	got, err = layers.GetHareOutput(tm.cdb, gPlus4)
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
	blocks1 := createLayerBlocks(t, tm.Mesh, gPlus1, false)
	tm.mockBeacon.EXPECT().GetBeacon(gPlus1.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus1).Return(gLyr)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), blocks1[0]).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus1, blocks1[0].ID()))
	hareOutput, err := layers.GetHareOutput(tm.cdb, gPlus1)
	require.NoError(t, err)
	require.Equal(t, blocks1[0].ID(), hareOutput)
	require.Equal(t, gPlus1, tm.ProcessedLayer())
	checkLastAppliedInDB(t, tm.Mesh, gPlus1)

	gPlus2 := gLyr.Add(2)
	createLayerBlocks(t, tm.Mesh, gPlus2, false)
	tm.mockBeacon.EXPECT().GetBeacon(gPlus2.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus2).Return(gPlus1)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), gPlus2, types.EmptyBlockID))

	// hare output saved
	hareOutput, err = layers.GetHareOutput(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.Equal(t, types.EmptyBlockID, hareOutput)

	// but processed layer has advanced
	require.Equal(t, gPlus2, tm.ProcessedLayer())
	require.Equal(t, gPlus2, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus2)
}

func TestMesh_ProcessLayer_BeaconDelayed(t *testing.T) {
	tm := createTestMesh(t)
	gLyr := types.GetEffectiveGenesis()
	lid := gLyr.Add(1)
	errUnknown := errors.New("unknown")

	certs := []*types.Certificate{
		{
			BlockID: types.BlockID{1, 2, 3},
		},
		{
			BlockID: types.BlockID{1, 2, 3},
		},
	}
	tm.mockTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
	tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.EmptyBeacon, errUnknown)
	tm.ProcessCertificates(context.TODO(), lid, certs)

	tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.EmptyBeacon, errUnknown)
	tm.mockTortoise.EXPECT().LatestComplete().Return(gLyr)
	require.ErrorIs(t, tm.ProcessLayer(context.TODO(), lid), errMissingHareOutput)
	require.Equal(t, lid, tm.ProcessedLayer())
	checkLastAppliedInDB(t, tm.Mesh, gLyr)

	tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockCertHandler.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, certs[0]).DoAndReturn(
		func(_ context.Context, _ types.LayerID, got *types.Certificate) error {
			require.NoError(t, layers.SetHareOutputWithCert(tm.cdb, lid, got))
			return nil
		},
	)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.ProcessLayer(context.TODO(), lid))
	checkLastAppliedInDB(t, tm.Mesh, lid)
}

func TestMesh_ProcessCertificates(t *testing.T) {
	tm := createTestMesh(t)
	lid := types.NewLayerID(11)
	certs := []*types.Certificate{
		{
			BlockID: types.BlockID{1, 2, 3},
		},
		{
			BlockID: types.BlockID{1, 2, 3},
		},
	}
	tm.mockTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
	tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockCertHandler.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, certs[0]).Return(nil)

	tm.ProcessCertificates(context.TODO(), lid, certs)
}

func TestMesh_ProcessCertificates_BeaconDelayed(t *testing.T) {
	tm := createTestMesh(t)
	lid := types.NewLayerID(11)
	certs := []*types.Certificate{
		{
			BlockID: types.BlockID{1, 2, 3},
		},
		{
			BlockID: types.BlockID{1, 2, 3},
		},
	}
	tm.mockTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
	tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.EmptyBeacon, errors.New("meh"))

	tm.ProcessCertificates(context.TODO(), lid, certs)
	tm.mu.Lock()
	defer tm.mu.Unlock()
	require.Equal(t, certs, tm.certCache[lid])
}

func TestMesh_ProcessCertificates_AlreadyVerified(t *testing.T) {
	tm := createTestMesh(t)
	lid := types.NewLayerID(11)
	certs := []*types.Certificate{
		{
			BlockID: types.BlockID{1, 2, 3},
		},
		{
			BlockID: types.BlockID{1, 2, 3},
		},
	}
	tm.mockTortoise.EXPECT().LatestComplete().Return(lid)

	tm.ProcessCertificates(context.TODO(), lid, certs)
	tm.mu.Lock()
	defer tm.mu.Unlock()
	require.Empty(t, tm.certCache[lid])
}

func TestMesh_ProcessCertificates_AlreadySaved(t *testing.T) {
	tm := createTestMesh(t)
	lid := types.NewLayerID(11)
	certs := []*types.Certificate{
		{
			BlockID: types.BlockID{1, 2, 3},
		},
		{
			BlockID: types.BlockID{1, 2, 3},
		},
	}
	require.NoError(t, layers.SetHareOutputWithCert(tm.cdb, lid, certs[0]))
	tm.mockTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
	tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)

	tm.ProcessCertificates(context.TODO(), lid, certs)
	tm.mu.Lock()
	defer tm.mu.Unlock()
	require.Empty(t, tm.certCache[lid])
}

func TestMesh_Revert(t *testing.T) {
	tm := createTestMesh(t)
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
		tm.mockBeacon.EXPECT().GetBeacon(i.GetEpoch()).Return(types.RandomBeacon(), nil)
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), i).Return(i.Sub(1))
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), hareOutput).Return(nil)
		tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
		require.NoError(t, tm.ProcessLayerPerHareOutput(context.TODO(), i, hareOutput.ID()))
	}
	require.Equal(t, gPlus3, tm.ProcessedLayer())
	require.Equal(t, gPlus3, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, gPlus3)

	oldHash, err := layers.GetAggregatedHash(tm.cdb, gPlus2)
	require.NoError(t, err)
	require.NotEqual(t, types.EmptyLayerHash, oldHash)

	// for layer gPlus2, every other block turns out to be valid
	layerBlocks[gPlus2] = sortBlocks(blocks2[1:])[0]
	for _, blk := range blocks2[1:] {
		require.NoError(t, tm.UpdateBlockValidity(blk.ID(), gPlus2, true))
	}
	for lyr, blk := range layerBlocks {
		require.NoError(t, tm.UpdateBlockValidity(blk.ID(), lyr, true))
	}

	tm.mockBeacon.EXPECT().GetBeacon(gPlus4.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus4).Return(gPlus3)
	tm.mockState.EXPECT().RevertState(gPlus1).Return(types.Hash32{}, nil)
	for i := gPlus2; !i.After(gPlus4); i = i.Add(1) {
		hareOutput := layerBlocks[i]
		tm.mockState.EXPECT().ApplyLayer(context.TODO(), hareOutput).Return(nil)
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
	tm.mockBeacon.EXPECT().GetBeacon(gPlus5.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gPlus5).Return(gPlus4)
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), blocks5[0]).Return(nil)
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
	tm.mockTortoise.EXPECT().OnBallot(gomock.Any()).AnyTimes()
	layerID := types.GetEffectiveGenesis().Add(1)
	createLayerBallots(t, tm.Mesh, layerID)

	txIDs := types.RandomTXSet(5)
	block1 := addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[:1])
	block2 := addBlockWithTXsToMesh(t, tm, layerID, true, txIDs[1:4])
	block3 := addBlockWithTXsToMesh(t, tm, layerID, false, txIDs[3:])
	addBlockWithTXsToMesh(t, tm, layerID, false, txIDs[4:])

	// set block3 to be hare output
	require.NoError(t, layers.SetHareOutput(tm.cdb, layerID, block3.ID()))

	valids := []*types.Block{block1, block2}
	sortBlocks(valids)
	toApply := valids[0]
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), toApply).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.pushLayersToState(context.TODO(), layerID, layerID, layerID))
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
				require.NoError(t, tm.Mesh.saveContextualValidity(block.ID(), lid, true))
			}

			tm.mockState.EXPECT().ApplyLayer(context.TODO(), tc.blocks[tc.expected]).Return(nil).Times(1)
			tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil).Times(1)
			require.NoError(t, tm.pushLayersToState(context.TODO(), lid, lid, lid))
		})
	}
}

func TestMesh_pushLayersToState_notVerified(t *testing.T) {
	tm := createTestMesh(t)
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
	require.NoError(t, layers.SetHareOutput(tm.cdb, layerID, hareOutput.ID()))

	tm.mockState.EXPECT().ApplyLayer(context.TODO(), hareOutput).Return(nil)
	tm.mockState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, tm.pushLayersToState(context.TODO(), layerID, layerID, layerID.Sub(1)))
	checkLastAppliedInDB(t, tm.Mesh, layerID)
}

func addBlockWithTXsToMesh(t *testing.T, tm *testMesh, id types.LayerID, valid bool, txIDs []types.TransactionID) *types.Block {
	t.Helper()
	b := types.GenLayerBlock(id, txIDs)
	tm.mockState.EXPECT().LinkTXsWithBlock(id, b.ID(), b.TxIDs)
	require.NoError(t, tm.Mesh.AddBlockWithTXs(context.TODO(), b))
	require.NoError(t, tm.Mesh.saveContextualValidity(b.ID(), id, valid))
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
	tm.mockTortoise.EXPECT().OnBallot(gomock.Any()).AnyTimes()

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
		require.NoError(t, layers.SetHareOutput(tm.cdb, lid, types.EmptyBlockID))
		tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}

	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, last)

	last = last.Add(1)
	tm.mockBeacon.EXPECT().GetBeacon(last.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last).Return(last.Sub(1))

	block := types.NewExistingBlock(types.BlockID{1},
		types.InnerBlock{LayerIndex: last, TxIDs: []types.TransactionID{{1, 1, 1}}})
	require.NoError(t, blocks.Add(tm.cdb, block))
	require.NoError(t, layers.SetHareOutput(tm.cdb, last, block.ID()))
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), block).Return(errTXMissing)
	require.Error(t, tm.ProcessLayer(ctx, last))
	require.Equal(t, last, tm.ProcessedLayer())
	require.Equal(t, last, tm.MissingLayer())
	require.Equal(t, last.Sub(1), tm.LatestLayerInState())
	checkLastAppliedInDB(t, tm.Mesh, last.Sub(1))

	last = last.Add(1)
	require.NoError(t, tm.saveContextualValidity(block.ID(), last.Sub(1), true))
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), block).Return(nil)
	require.NoError(t, layers.SetHareOutput(tm.cdb, last, types.EmptyBlockID))
	for lid := last.Sub(1); !lid.After(last); lid = lid.Add(1) {
		tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(last.Sub(1))
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
	require.NoError(t, layers.SetHareOutput(tm.cdb, last, block.ID()))

	tm.mockBeacon.EXPECT().GetBeacon(last.GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last).Return(last.Sub(1))
	errTXMissing := errors.New("tx missing")
	tm.mockState.EXPECT().ApplyLayer(context.TODO(), block).Return(errTXMissing)
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
		require.NoError(t, layers.SetHareOutput(tm.cdb, lid, types.EmptyBlockID))
		tm.mockBeacon.EXPECT().GetBeacon(lid.GetEpoch()).Return(types.RandomBeacon(), nil)
		tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
		tm.mockState.EXPECT().GetStateRoot()
		require.NoError(t, tm.ProcessLayer(ctx, lid))
	}
	require.Equal(t, last, tm.LatestLayerInState())

	require.NoError(t, layers.SetHareOutput(tm.cdb, last.Add(1), types.EmptyBlockID))
	tm.mockBeacon.EXPECT().GetBeacon(last.Add(1).GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last.Add(1)).Return(tm.LatestLayerInState().Sub(1))
	tm.mockState.EXPECT().GetStateRoot()
	require.NoError(t, tm.ProcessLayer(ctx, last.Add(1)))
	require.Equal(t, last.Add(1), tm.LatestLayerInState())

	tm.mockBeacon.EXPECT().GetBeacon(last.Add(2).GetEpoch()).Return(types.RandomBeacon(), nil)
	tm.mockTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), last.Add(2)).Return(last.Add(1))
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

func TestMesh_CallOnBallot(t *testing.T) {
	tm := createTestMesh(t)
	ballot := types.RandomBallot()
	tm.mockTortoise.EXPECT().OnBallot(ballot)
	require.NoError(t, tm.AddBallot(ballot))
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
	require.NoError(t, tm.addBallot(&blts[0]))
	require.False(t, blts[0].IsMalicious())
	for _, ballot := range blts[1:] {
		require.NoError(t, tm.addBallot(&ballot))
		require.True(t, ballot.IsMalicious())
	}
}
