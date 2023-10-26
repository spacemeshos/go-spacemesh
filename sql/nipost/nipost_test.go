package nipost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestGet(t *testing.T) {
	db := sql.InMemory()

	commitmentATX := types.RandomATXID()

	tt := []struct {
		name string
		ch   *types.NIPostChallenge
	}{
		{
			name: "nil commitment ATX & post",
			ch: &types.NIPostChallenge{
				PublishEpoch:   4,
				Sequence:       2,
				PrevATXID:      types.RandomATXID(),
				PositioningATX: types.RandomATXID(),
				CommitmentATX:  nil,
				InitialPost:    nil,
			},
		},
		{
			name: "commitment and initial post",
			ch: &types.NIPostChallenge{
				PublishEpoch:   77,
				Sequence:       13,
				PrevATXID:      types.RandomATXID(),
				PositioningATX: types.RandomATXID(),
				CommitmentATX:  &commitmentATX,
				InitialPost:    &types.Post{Nonce: 1, Indices: []byte{1, 2, 3}, Pow: 1},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			nodeID := types.RandomNodeID()
			err := AddChallenge(db, nodeID, tc.ch)
			require.NoError(t, err)

			challenge, err := ByNodeIDAndEpoch(db, nodeID, tc.ch.PublishEpoch)
			require.NoError(t, err)
			require.NotNil(t, challenge)
			require.Equal(t, tc.ch, challenge)
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
		Sequence:       2,
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
}
