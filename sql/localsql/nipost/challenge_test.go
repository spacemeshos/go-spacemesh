package nipost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func Test_AddChallenge(t *testing.T) {
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
			db := localsql.InMemoryTest(t)

			nodeID := types.RandomNodeID()
			err := AddChallenge(db, nodeID, tc.ch)
			require.NoError(t, err)

			challenge, err := Challenge(db, nodeID)
			require.NoError(t, err)
			require.NotNil(t, challenge)
			require.Equal(t, tc.ch, challenge)

			err = RemoveChallenge(db, nodeID)
			require.NoError(t, err)

			challenge, err = Challenge(db, nodeID)
			require.ErrorIs(t, err, sql.ErrNotFound)
			require.Nil(t, challenge)
		})
	}
}

func Test_AddChallenge_NoDuplicates(t *testing.T) {
	db := localsql.InMemoryTest(t)

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

	// fail to add challenge for same node
	err = AddChallenge(db, nodeID, ch2)
	require.ErrorIs(t, err, sql.ErrObjectExists)

	// succeed to add challenge for different node
	err = AddChallenge(db, types.RandomNodeID(), ch2)
	require.NoError(t, err)
}

func Test_UpdateChallenge(t *testing.T) {
	db := localsql.InMemoryTest(t)

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
	err = UpdateChallenge(db, nodeID, ch)
	require.NoError(t, err)

	challenge, err := Challenge(db, nodeID)
	require.NoError(t, err)
	require.NotNil(t, challenge)
	require.Equal(t, ch, challenge)
}

func Test_UpdatePoetProofRef(t *testing.T) {
	db := localsql.InMemoryTest(t)

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

	var refPoetProofRef types.PoetProofRef
	copy(refPoetProofRef[:], types.RandomHash().Bytes())
	refMembership := &types.MerkleProof{
		Nodes:     []types.Hash32{types.RandomHash(), types.RandomHash()},
		LeafIndex: 1,
	}
	err = UpdatePoetProofRef(db, nodeID, refPoetProofRef, refMembership)
	require.NoError(t, err)

	// update challenge
	poetProofRef, membership, err := PoetProofRef(db, nodeID)
	require.NoError(t, err)
	require.Equal(t, refPoetProofRef, poetProofRef)
	require.Equal(t, refMembership, membership)
}

func Test_PoetProofRef_NotSet(t *testing.T) {
	db := localsql.InMemoryTest(t)

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

	poetProofRef, membership, err := PoetProofRef(db, nodeID)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Equal(t, types.PoetProofRef{}, poetProofRef)
	require.Nil(t, membership)
}
