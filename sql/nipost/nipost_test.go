package nipost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestChallengeByEpoch(t *testing.T) {
	commitmentATX := types.RandomATXID()
	tt := []struct {
		name string
		ch   *types.NIPostChallenge
	}{
		{
			name: "nil commitment ATX & post",
			ch: &types.NIPostChallenge{
				PublishEpoch:   4,
				Sequence:       0,
				PrevATXID:      types.RandomATXID(),
				PositioningATX: types.RandomATXID(),
				CommitmentATX:  &commitmentATX,
				InitialPost:    &types.Post{Nonce: 1, Indices: []byte{1, 2, 3}, Pow: 1},
			},
		},
		{
			name: "commitment and initial post",
			ch: &types.NIPostChallenge{
				PublishEpoch:   77,
				Sequence:       13,
				PrevATXID:      types.RandomATXID(),
				PositioningATX: types.RandomATXID(),
				CommitmentATX:  nil,
				InitialPost:    nil,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			db := sql.InMemory()

			nodeID := types.RandomNodeID()
			err := AddChallenge(db, nodeID, tc.ch)
			require.NoError(t, err)

			challenge, err := ChallengeByEpoch(db, nodeID, tc.ch.PublishEpoch)
			require.NoError(t, err)
			require.NotNil(t, challenge)
			require.Equal(t, tc.ch, challenge)

			err = RemoveChallenge(db, nodeID)
			require.NoError(t, err)

			challenge, err = ChallengeByEpoch(db, nodeID, tc.ch.PublishEpoch)
			require.ErrorIs(t, err, sql.ErrNotFound)
			require.Nil(t, challenge)
		})
	}
}

func TestAdd_NoDuplicateEpoch(t *testing.T) {
	db := sql.InMemory()

	ch1 := &types.NIPostChallenge{
		PublishEpoch:   4,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  nil,
		InitialPost:    nil,
	}
	ch2 := &types.NIPostChallenge{
		PublishEpoch:   4,
		Sequence:       3,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  nil,
		InitialPost:    nil,
	}

	nodeID := types.RandomNodeID()
	err := AddChallenge(db, nodeID, ch1)
	require.NoError(t, err)

	// fail to add challenge with same epoch for same node
	err = AddChallenge(db, nodeID, ch2)
	require.Error(t, err)

	// succeed to add challenge with same epoch for different node
	err = AddChallenge(db, types.RandomNodeID(), ch2)
	require.NoError(t, err)

	// succeed to add challenge with different epoch for same node
	ch2.PublishEpoch = 5
	err = AddChallenge(db, nodeID, ch2)
	require.NoError(t, err)
}

func TestChallengeBySequence(t *testing.T) {
	commitmentATX := types.RandomATXID()
	tt := []struct {
		name string
		ch   *types.NIPostChallenge
	}{
		{
			name: "nil commitment ATX & post",
			ch: &types.NIPostChallenge{
				PublishEpoch:   4,
				Sequence:       0,
				PrevATXID:      types.RandomATXID(),
				PositioningATX: types.RandomATXID(),
				CommitmentATX:  &commitmentATX,
				InitialPost:    &types.Post{Nonce: 1, Indices: []byte{1, 2, 3}, Pow: 1},
			},
		},
		{
			name: "commitment and initial post",
			ch: &types.NIPostChallenge{
				PublishEpoch:   77,
				Sequence:       13,
				PrevATXID:      types.RandomATXID(),
				PositioningATX: types.RandomATXID(),
				CommitmentATX:  nil,
				InitialPost:    nil,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			db := sql.InMemory()

			nodeID := types.RandomNodeID()
			err := AddChallenge(db, nodeID, tc.ch)
			require.NoError(t, err)

			challenge, err := ChallengeBySequence(db, nodeID, tc.ch.Sequence)
			require.NoError(t, err)
			require.NotNil(t, challenge)
			require.Equal(t, tc.ch, challenge)

			err = RemoveChallenge(db, nodeID)
			require.NoError(t, err)

			challenge, err = ChallengeBySequence(db, nodeID, tc.ch.Sequence)
			require.ErrorIs(t, err, sql.ErrNotFound)
			require.Nil(t, challenge)
		})
	}
}

func TestUpdateChallenge(t *testing.T) {
	db := sql.InMemory()

	commitmentATX := types.RandomATXID()
	ch := &types.NIPostChallenge{
		PublishEpoch:   6,
		Sequence:       0,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  &commitmentATX,
		InitialPost:    &types.Post{Nonce: 1, Indices: []byte{1, 2, 3}, Pow: 1},
	}

	nodeID := types.RandomNodeID()
	err := AddChallenge(db, nodeID, ch)
	require.NoError(t, err)

	// update challenge
	ch.PublishEpoch = 7
	err = UpdateChallengeBySequence(db, nodeID, ch)
	require.NoError(t, err)

	challenge, err := ChallengeByEpoch(db, nodeID, ch.PublishEpoch)
	require.NoError(t, err)
	require.NotNil(t, challenge)
	require.Equal(t, ch, challenge)
}

func TestAdd_NoDuplicateSequence(t *testing.T) {
	db := sql.InMemory()

	ch1 := &types.NIPostChallenge{
		PublishEpoch:   4,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  nil,
		InitialPost:    nil,
	}
	ch2 := &types.NIPostChallenge{
		PublishEpoch:   5,
		Sequence:       2,
		PrevATXID:      types.RandomATXID(),
		PositioningATX: types.RandomATXID(),
		CommitmentATX:  nil,
		InitialPost:    nil,
	}

	nodeID := types.RandomNodeID()
	err := AddChallenge(db, nodeID, ch1)
	require.NoError(t, err)

	// fail to add challenge with same sequence for same node
	err = AddChallenge(db, nodeID, ch2)
	require.Error(t, err)

	// succeed to add challenge with same sequence for different node
	err = AddChallenge(db, types.RandomNodeID(), ch2)
	require.NoError(t, err)

	// add challenge with different sequence for same node
	ch2.Sequence = 4
	err = AddChallenge(db, nodeID, ch2)
	require.NoError(t, err)
}
