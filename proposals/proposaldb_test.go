package proposals

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type testDB struct {
	*DB
	mesh *mesh.Mesh
}

func createTestDB(t *testing.T) *testDB {
	types.SetLayersPerEpoch(layersPerEpoch)

	db := sql.InMemory()
	mdb, err := mesh.NewPersistentMeshDB(db, 1, logtest.New(t))
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockTortoise := mocks.NewMocktortoise(ctrl)
	mockTortoise.EXPECT().OnBallot(gomock.Any()).AnyTimes()

	mockState := mocks.NewMockconservativeState(ctrl)
	mockState.EXPECT().StoreTransactionsFromMemPool(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	m := mesh.NewMesh(mdb, nil, mockTortoise, mockState, logtest.New(t))

	td := &testDB{
		mesh: m,
	}
	td.DB = newDB(
		withSQLDB(db),
		withMeshDB(td.mesh),
		withLogger(logtest.New(t)))
	return td
}

func TestDB_AddProposal(t *testing.T) {
	td := createTestDB(t)
	defer td.mesh.DB.Close()

	layerID := types.GetEffectiveGenesis().Add(10)
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	require.NoError(t, td.AddProposal(context.TODO(), p))
}

func TestDB_HasProposal(t *testing.T) {
	td := createTestDB(t)
	defer td.mesh.DB.Close()

	layerID := types.GetEffectiveGenesis().Add(10)
	p1 := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	p2 := types.GenLayerProposal(layerID, types.RandomTXSet(101))

	require.False(t, td.HasProposal(p1.ID()))
	require.False(t, td.HasProposal(p2.ID()))

	require.NoError(t, td.AddProposal(context.TODO(), p1))

	require.True(t, td.HasProposal(p1.ID()))
	require.False(t, td.HasProposal(p2.ID()))
}

func TestDB_GetProposal(t *testing.T) {
	td := createTestDB(t)
	defer td.mesh.DB.Close()

	layerID := types.GetEffectiveGenesis().Add(10)
	p1 := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	p2 := types.GenLayerProposal(layerID, types.RandomTXSet(101))

	got1, err := td.GetProposal(p1.ID())
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, got1)

	got2, err := td.GetProposal(p2.ID())
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, got2)

	require.NoError(t, td.AddProposal(context.TODO(), p1))

	got1, err = td.GetProposal(p1.ID())
	require.NoError(t, err)
	require.Equal(t, p1, got1)

	got2, err = td.GetProposal(p2.ID())
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, got2)
}

func createAndAddProposals(t *testing.T, td *testDB, layerID types.LayerID) *types.Proposal {
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	require.NoError(t, td.AddProposal(context.TODO(), p))
	return p
}

func TestDB_Get(t *testing.T) {
	td := createTestDB(t)
	defer td.mesh.DB.Close()

	layerID := types.GetEffectiveGenesis().Add(10)
	numProposals := 100
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := createAndAddProposals(t, td, layerID)
		proposals = append(proposals, p)
	}

	for i := 0; i < numProposals; i++ {
		hash := proposals[i].ID().AsHash32()
		data, err := td.Get(hash.Bytes())
		require.NoError(t, err)
		var got types.Proposal
		require.NoError(t, codec.Decode(data, &got))
		require.NoError(t, got.Initialize())
		require.Equal(t, *proposals[i], got)
	}
}

func TestDB_GetProposals(t *testing.T) {
	td := createTestDB(t)
	defer td.mesh.DB.Close()

	layerID := types.GetEffectiveGenesis().Add(10)
	numProposals := 100
	proposals := make([]*types.Proposal, 0, numProposals)
	for i := 0; i < numProposals; i++ {
		p := createAndAddProposals(t, td, layerID)
		proposals = append(proposals, p)
	}

	pids := types.ToProposalIDs(proposals)
	got, err := td.GetProposals(pids)
	require.NoError(t, err)
	require.Equal(t, proposals, got)
}

func TestDB_LayerProposalIDs(t *testing.T) {
	td := createTestDB(t)
	defer td.mesh.DB.Close()

	layerID := types.GetEffectiveGenesis().Add(10)
	numLayers := uint32(5)
	numProposals := 100
	lyrToProposals := make(map[types.LayerID][]*types.Proposal, numLayers)
	for lyr := layerID; lyr.Before(layerID.Add(numLayers)); lyr = lyr.Add(1) {
		proposals := make([]*types.Proposal, 0, numProposals)
		for i := 0; i < numProposals; i++ {
			p := createAndAddProposals(t, td, lyr)
			proposals = append(proposals, p)
		}
		lyrToProposals[lyr] = proposals
	}

	for lyr := layerID; lyr.Before(layerID.Add(numLayers)); lyr = lyr.Add(1) {
		pids := types.ToProposalIDs(lyrToProposals[lyr])
		got, err := td.LayerProposalIDs(lyr)
		require.NoError(t, err)
		require.Equal(t, types.SortProposalIDs(pids), types.SortProposalIDs(got))
	}

	got, err := td.LayerProposalIDs(layerID.Add(numLayers + 1))
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Empty(t, got)
}

func TestDB_LayerProposals(t *testing.T) {
	td := createTestDB(t)
	defer td.mesh.DB.Close()

	layerID := types.GetEffectiveGenesis().Add(10)
	numLayers := uint32(5)
	numProposals := 100
	lyrToProposals := make(map[types.LayerID][]*types.Proposal, numLayers)
	for lyr := layerID; lyr.Before(layerID.Add(numLayers)); lyr = lyr.Add(1) {
		proposals := make([]*types.Proposal, 0, numProposals)
		for i := 0; i < numProposals; i++ {
			p := createAndAddProposals(t, td, lyr)
			proposals = append(proposals, p)
		}
		lyrToProposals[lyr] = proposals
	}

	for lyr := layerID; lyr.Before(layerID.Add(numLayers)); lyr = lyr.Add(1) {
		proposals := lyrToProposals[lyr]
		got, err := td.LayerProposals(lyr)
		require.NoError(t, err)
		require.Equal(t, types.SortProposals(proposals), types.SortProposals(got))
	}

	got, err := td.LayerProposals(layerID.Add(numLayers + 1))
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Empty(t, got)
}
