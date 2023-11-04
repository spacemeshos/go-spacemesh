package nipost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Test_AddInitialPost(t *testing.T) {
	db := datastore.NewLocalDB(sql.InMemory(sql.WithMigrations(sql.LocalMigrations)))

	nodeID := types.RandomNodeID()
	post := &types.Post{
		Nonce:   1,
		Indices: []byte{1, 2, 3},
		Pow:     1,
	}
	commitmentATX := types.RandomATXID()
	err := AddInitialPost(db, nodeID, post, commitmentATX)
	require.NoError(t, err)

	post, atx, err := InitialPost(db, nodeID)
	require.NoError(t, err)
	require.NotNil(t, post)
	require.Equal(t, post, post)
	require.Equal(t, commitmentATX, atx)

	err = RemoveInitialPost(db, nodeID)
	require.NoError(t, err)

	post, atx, err = InitialPost(db, nodeID)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, post)
	require.Equal(t, types.EmptyATXID, atx)
}

func Test_AddInitialPost_NoDuplicates(t *testing.T) {
	db := datastore.NewLocalDB(sql.InMemory(sql.WithMigrations(sql.LocalMigrations)))

	nodeID := types.RandomNodeID()
	post := &types.Post{
		Nonce:   1,
		Indices: []byte{1, 2, 3},
		Pow:     1,
	}
	commitmentATX := types.RandomATXID()
	err := AddInitialPost(db, nodeID, post, commitmentATX)
	require.NoError(t, err)

	// fail to add new initial post for same node
	post2 := &types.Post{
		Nonce:   2,
		Indices: []byte{1, 2, 3},
		Pow:     1,
	}
	commitmentATX2 := types.RandomATXID()
	err = AddInitialPost(db, nodeID, post2, commitmentATX2)
	require.Error(t, err)

	// succeed to add initial post for different node
	err = AddInitialPost(db, types.RandomNodeID(), post2, commitmentATX2)
	require.NoError(t, err)
}

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
			db := datastore.NewLocalDB(sql.InMemory(sql.WithMigrations(sql.LocalMigrations)))

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

func Test_AddState_NoDuplicates(t *testing.T) {
	db := datastore.NewLocalDB(sql.InMemory(sql.WithMigrations(sql.LocalMigrations)))

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
	require.Error(t, err)

	// succeed to add challenge for different node
	err = AddChallenge(db, types.RandomNodeID(), ch2)
	require.NoError(t, err)
}

func Test_UpdateState(t *testing.T) {
	db := datastore.NewLocalDB(sql.InMemory(sql.WithMigrations(sql.LocalMigrations)))

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
