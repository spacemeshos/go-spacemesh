package store_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
)

func generateProposal(layer types.LayerID) *types.Proposal {
	proposal := types.Proposal{}
	proposal.Layer = layer
	proposal.EpochData = &types.EpochData{
		Beacon: types.RandomBeacon(),
	}
	proposal.AtxID = types.RandomATXID()
	proposal.SmesherID = types.RandomNodeID()
	proposal.Ballot.SmesherID = proposal.SmesherID
	proposal.SetID(types.RandomProposalID())
	proposal.Ballot.SetID(types.RandomBallotID())
	return &proposal
}

func TestStore_Add(t *testing.T) {
	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		s := store.New()
		proposal := generateProposal(types.LayerID(0))
		require.NoError(t, s.Add(proposal))

		require.True(t, s.Has(proposal.ID()))
		got := s.Get(proposal.Layer, proposal.ID())
		require.Equal(t, proposal, got)

		require.False(t, s.Has(types.RandomProposalID()))
	})
	t.Run("cannot add in evicted layer", func(t *testing.T) {
		t.Parallel()
		s := store.New(store.WithCapacity(2))
		proposal := generateProposal(types.LayerID(5))
		s.OnLayer(types.LayerID(7))
		require.Equal(t, store.ErrLayerEvicted, s.Add(proposal))
		require.Nil(t, s.Get(proposal.Layer, proposal.ID()))
	})
	t.Run("cannot add a duplicate", func(t *testing.T) {
		t.Parallel()
		s := store.New()
		proposal := generateProposal(types.LayerID(0))
		require.NoError(t, s.Add(proposal))
		require.Equal(t, store.ErrProposalExists, s.Add(proposal))
	})
}

func TestStore_GetMany(t *testing.T) {
	t.Run("all available", func(t *testing.T) {
		t.Parallel()
		s := store.New()
		proposal1 := generateProposal(types.LayerID(0))
		proposal2 := generateProposal(types.LayerID(0))
		require.NoError(t, s.Add(proposal1))
		require.NoError(t, s.Add(proposal2))
		got := s.GetMany(types.LayerID(0), proposal1.ID(), proposal2.ID())
		require.ElementsMatch(t, []*types.Proposal{proposal1, proposal2}, got)
	})
	t.Run("missing proposal", func(t *testing.T) {
		t.Parallel()
		s := store.New()
		proposal := generateProposal(types.LayerID(0))
		require.NoError(t, s.Add(proposal))
		got := s.GetMany(types.LayerID(0), types.RandomProposalID(), proposal.ID(), types.RandomProposalID())
		require.Equal(t, []*types.Proposal{proposal}, got)
	})
	t.Run("from ectived layer", func(t *testing.T) {
		t.Parallel()
		s := store.New(store.WithCapacity(2))
		proposal := generateProposal(types.LayerID(5))
		require.NoError(t, s.Add(proposal))
		require.True(t, s.Has(proposal.ID()))

		s.OnLayer(types.LayerID(7))
		got := s.GetMany(proposal.Layer, proposal.ID())
		require.Nil(t, got)
	})
}

func TestStore_GetBlob(t *testing.T) {
	t.Run("get blob", func(t *testing.T) {
		t.Parallel()
		s := store.New()
		proposal := generateProposal(types.LayerID(0))
		require.NoError(t, s.Add(proposal))
		size, err := s.GetBlobSize(proposal.ID())
		require.NoError(t, err)
		got, err := s.GetBlob(proposal.ID())
		require.NoError(t, err)
		encoded := codec.MustEncode(proposal)
		require.Equal(t, encoded, got)
		require.Equal(t, len(encoded), size)
	})
	t.Run("get blob when missing", func(t *testing.T) {
		t.Parallel()
		s := store.New()
		_, err := s.GetBlobSize(types.RandomProposalID())
		require.ErrorIs(t, err, store.ErrNotFound)
		got, err := s.GetBlob(types.RandomProposalID())
		require.Nil(t, got)
		require.ErrorIs(t, err, store.ErrNotFound)
	})
}

func TestStore_Evicting(t *testing.T) {
	t.Parallel()
	s := store.New(store.WithCapacity(2))
	proposal := generateProposal(types.LayerID(0))
	require.NoError(t, s.Add(proposal))
	require.True(t, s.Has(proposal.ID()))
	s.OnLayer(types.LayerID(2))
	require.False(t, s.Has(proposal.ID()))
}

func TestStore_GetForLayer(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		s := store.New()
		proposal1 := generateProposal(types.LayerID(0))
		proposal2 := generateProposal(types.LayerID(0))
		require.NoError(t, s.Add(proposal1))
		require.NoError(t, s.Add(proposal2))
		got := s.GetForLayer(types.LayerID(0))
		require.ElementsMatch(t, []*types.Proposal{proposal1, proposal2}, got)

		got = s.GetForLayer(types.LayerID(7))
		require.Empty(t, got)
	})
	t.Run("evicted layer", func(t *testing.T) {
		t.Parallel()
		s := store.New(store.WithCapacity(2))
		proposal := generateProposal(types.LayerID(0))
		require.NoError(t, s.Add(proposal))
		s.OnLayer(2)

		got := s.GetForLayer(types.LayerID(0))
		require.Empty(t, got)
	})
}

func TestStore_WithFirstLayer(t *testing.T) {
	t.Parallel()
	s := store.New(store.WithEvictedLayer(5))
	require.ErrorIs(t, s.Add(generateProposal(types.LayerID(0))), store.ErrLayerEvicted)
	require.ErrorIs(t, s.Add(generateProposal(types.LayerID(5))), store.ErrLayerEvicted)
	require.NoError(t, s.Add(generateProposal(types.LayerID(6))))
}
