package proposals

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/proposals/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
)

type testDB struct {
	*DB
	mockMesh *mocks.MockmeshDB
}

func createTestDB(t *testing.T) *testDB {
	types.SetLayersPerEpoch(layersPerEpoch)

	ctrl := gomock.NewController(t)
	td := &testDB{
		mockMesh: mocks.NewMockmeshDB(ctrl),
	}
	td.DB = newDB(
		withSQLDB(sql.InMemory()),
		withMeshDB(td.mockMesh),
		withLogger(logtest.New(t)))
	return td
}

func TestDB_AddProposal_FailToAddBallot(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	errUnknown := errors.New("unknown")
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).Return(errUnknown).Times(1)
	require.ErrorIs(t, td.AddProposal(context.TODO(), p), errUnknown)
}

func TestDB_AddProposal_FailToAddTXFromProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(errUnknown).Times(1)
	require.ErrorIs(t, td.AddProposal(context.TODO(), p), errUnknown)
}

func TestDB_AddProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, td.AddProposal(context.TODO(), p))
}

func TestDB_HasProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p1 := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	p2 := types.GenLayerProposal(layerID, types.RandomTXSet(101))

	require.False(t, td.HasProposal(p1.ID()))
	require.False(t, td.HasProposal(p2.ID()))

	td.mockMesh.EXPECT().AddBallot(&p1.Ballot).Return(nil).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p1.LayerIndex, p1.ID(), p1.TxIDs).Return(nil).Times(1)
	require.NoError(t, td.AddProposal(context.TODO(), p1))

	require.True(t, td.HasProposal(p1.ID()))
	require.False(t, td.HasProposal(p2.ID()))
}

func TestDB_GetProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p1 := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	p2 := types.GenLayerProposal(layerID, types.RandomTXSet(101))

	got1, err := td.GetProposal(p1.ID())
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, got1)

	got2, err := td.GetProposal(p2.ID())
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, got2)

	td.mockMesh.EXPECT().AddBallot(&p1.Ballot).DoAndReturn(func(b *types.Ballot) error {
		return ballots.Add(td.sqlDB, b)
	}).Times(1)

	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p1.LayerIndex, p1.ID(), p1.TxIDs).Return(nil).Times(1)
	require.NoError(t, td.AddProposal(context.TODO(), p1))

	got1, err = td.GetProposal(p1.ID())
	require.NoError(t, err)
	got1.Ballot = p1.Ballot
	require.Equal(t, p1, got1)

	got2, err = td.GetProposal(p2.ID())
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, got2)
}

func createAndAddProposals(t *testing.T, td *testDB, layerID types.LayerID) *types.Proposal {
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).DoAndReturn(func(b *types.Ballot) error {
		return ballots.Add(td.sqlDB, b)
	}).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	require.NoError(t, td.AddProposal(context.TODO(), p))
	return p
}

func TestDB_Get(t *testing.T) {
	td := createTestDB(t)
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
		got.Ballot = (*proposals[i]).Ballot
		got.SetID((*proposals[i]).ID())
		require.Equal(t, *proposals[i], got)
	}
}

func TestDB_GetProposals(t *testing.T) {
	td := createTestDB(t)
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
	require.EqualValues(t, proposals, got)
}

func TestDB_LayerProposalIDs(t *testing.T) {
	td := createTestDB(t)
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
