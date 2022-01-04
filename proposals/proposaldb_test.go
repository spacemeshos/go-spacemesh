package proposals

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/proposals/mocks"
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
		withProposalDB(database.NewMemDatabase()),
		withLayerDB(database.NewMemDatabase()),
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
	assert.ErrorIs(t, td.AddProposal(context.TODO(), p), errUnknown)
}

func TestDB_AddProposal_FailToAddTXFromProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	errUnknown := errors.New("unknown")
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(errUnknown).Times(1)
	assert.ErrorIs(t, td.AddProposal(context.TODO(), p), errUnknown)
}

func TestDB_AddProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	assert.NoError(t, td.AddProposal(context.TODO(), p))
}

func TestDB_HasProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p1 := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	p2 := types.GenLayerProposal(layerID, types.RandomTXSet(101))

	assert.False(t, td.HasProposal(p1.ID()))
	assert.False(t, td.HasProposal(p2.ID()))

	td.mockMesh.EXPECT().AddBallot(&p1.Ballot).Return(nil).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p1.LayerIndex, p1.ID(), p1.TxIDs).Return(nil).Times(1)
	assert.NoError(t, td.AddProposal(context.TODO(), p1))

	assert.True(t, td.HasProposal(p1.ID()))
	assert.False(t, td.HasProposal(p2.ID()))
}

func TestDB_GetProposal(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p1 := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	p2 := types.GenLayerProposal(layerID, types.RandomTXSet(101))

	got1, err := td.GetProposal(p1.ID())
	assert.ErrorIs(t, err, database.ErrNotFound)
	assert.Nil(t, got1)

	got2, err := td.GetProposal(p2.ID())
	assert.ErrorIs(t, err, database.ErrNotFound)
	assert.Nil(t, got2)

	td.mockMesh.EXPECT().AddBallot(&p1.Ballot).Return(nil).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p1.LayerIndex, p1.ID(), p1.TxIDs).Return(nil).Times(1)
	assert.NoError(t, td.AddProposal(context.TODO(), p1))

	td.mockMesh.EXPECT().GetBallot(p1.Ballot.ID()).Return(&p1.Ballot, nil).Times(1)
	got1, err = td.GetProposal(p1.ID())
	assert.NoError(t, err)
	assert.Equal(t, p1, got1)

	got2, err = td.GetProposal(p2.ID())
	assert.ErrorIs(t, err, database.ErrNotFound)
	assert.Nil(t, got2)
}

func TestDB_GetProposal_GetBallotError(t *testing.T) {
	td := createTestDB(t)
	layerID := types.GetEffectiveGenesis().Add(10)
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	assert.NoError(t, td.AddProposal(context.TODO(), p))

	errUnknown := errors.New("unknown")
	td.mockMesh.EXPECT().GetBallot(p.Ballot.ID()).Return(nil, errUnknown).Times(1)
	got, err := td.GetProposal(p.ID())
	assert.ErrorIs(t, err, errUnknown)
	assert.Nil(t, got)
}

func createAndAddProposals(t *testing.T, td *testDB, layerID types.LayerID) *types.Proposal {
	p := types.GenLayerProposal(layerID, types.RandomTXSet(100))
	td.mockMesh.EXPECT().AddBallot(&p.Ballot).Return(nil).Times(1)
	td.mockMesh.EXPECT().AddTXsFromProposal(gomock.Any(), p.LayerIndex, p.ID(), p.TxIDs).Return(nil).Times(1)
	assert.NoError(t, td.AddProposal(context.TODO(), p))
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
		td.mockMesh.EXPECT().GetBallot(p.Ballot.ID()).Return(&p.Ballot, nil).Times(1)
	}

	for i := 0; i < numProposals; i++ {
		hash := proposals[i].ID().AsHash32()
		data, err := td.Get(hash.Bytes())
		assert.NoError(t, err)
		var got types.Proposal
		require.NoError(t, codec.Decode(data, &got))
		require.NoError(t, got.Initialize())
		assert.Equal(t, *proposals[i], got)
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
		td.mockMesh.EXPECT().GetBallot(p.Ballot.ID()).Return(&p.Ballot, nil).Times(1)
	}

	pids := types.ToProposalIDs(proposals)
	got, err := td.GetProposals(pids)
	assert.NoError(t, err)
	assert.Equal(t, proposals, got)
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
		assert.NoError(t, err)
		assert.Equal(t, types.SortProposalIDs(pids), types.SortProposalIDs(got))
	}

	got, err := td.LayerProposalIDs(layerID.Add(numLayers + 1))
	assert.ErrorIs(t, err, database.ErrNotFound)
	assert.Empty(t, got)
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
			td.mockMesh.EXPECT().GetBallot(p.Ballot.ID()).Return(&p.Ballot, nil).Times(1)
		}
		lyrToProposals[lyr] = proposals
	}

	for lyr := layerID; lyr.Before(layerID.Add(numLayers)); lyr = lyr.Add(1) {
		proposals := lyrToProposals[lyr]
		got, err := td.LayerProposals(lyr)
		assert.NoError(t, err)
		assert.Equal(t, types.SortProposals(proposals), types.SortProposals(got))
	}

	got, err := td.LayerProposals(layerID.Add(numLayers + 1))
	assert.ErrorIs(t, err, database.ErrNotFound)
	assert.Empty(t, got)
}
