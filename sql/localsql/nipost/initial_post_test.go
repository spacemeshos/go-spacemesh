package nipost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func Test_AddInitialPost(t *testing.T) {
	db := localsql.InMemory()

	nodeID := types.RandomNodeID()
	post := Post{
		Nonce:   1,
		Indices: []byte{1, 2, 3},
		Pow:     1,

		NumUnits:      2,
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      3,
	}
	err := AddInitialPost(db, nodeID, post)
	require.NoError(t, err)

	got, err := InitialPost(db, nodeID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, post, *got)

	err = RemoveInitialPost(db, nodeID)
	require.NoError(t, err)

	got, err = InitialPost(db, nodeID)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)
}

func Test_AddInitialPost_NoDuplicates(t *testing.T) {
	db := localsql.InMemory()

	nodeID := types.RandomNodeID()
	post := Post{
		Nonce:   1,
		Indices: []byte{1, 2, 3},
		Pow:     1,

		NumUnits:      2,
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      3,
	}
	err := AddInitialPost(db, nodeID, post)
	require.NoError(t, err)

	// fail to add new initial post for same node
	post2 := Post{
		Nonce:   2,
		Indices: []byte{1, 2, 3},
		Pow:     1,

		NumUnits:      4,
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      5,
	}
	err = AddInitialPost(db, nodeID, post2)
	require.Error(t, err)

	// succeed to add initial post for different node
	err = AddInitialPost(db, types.RandomNodeID(), post2)
	require.NoError(t, err)
}
