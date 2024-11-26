package nipost

import (
	"testing"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func Test_AddPost(t *testing.T) {
	db := localsql.InMemoryTest(t)

	nodeID := types.RandomNodeID()
	post := Post{
		Nonce:     1,
		Indices:   []byte{1, 2, 3},
		Pow:       1,
		Challenge: shared.Challenge([]byte{4, 5, 6}),

		NumUnits:      2,
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      3,
	}
	err := AddPost(db, nodeID, post)
	require.NoError(t, err)

	got, err := GetPost(db, nodeID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, post, *got)
}

func Test_AddPost_NoDuplicates(t *testing.T) {
	db := localsql.InMemoryTest(t)

	nodeID := types.RandomNodeID()
	post := Post{
		Nonce:     1,
		Indices:   []byte{1, 2, 3},
		Pow:       1,
		Challenge: shared.ZeroChallenge,

		NumUnits:      2,
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      3,
	}
	err := AddPost(db, nodeID, post)
	require.NoError(t, err)

	// fail to add new post for same node
	post2 := Post{
		Nonce:     2,
		Indices:   []byte{1, 2, 3},
		Pow:       1,
		Challenge: shared.ZeroChallenge,

		NumUnits:      4,
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      5,
	}
	err = AddPost(db, nodeID, post2)
	require.Error(t, err)

	// succeed to add initial post for different node
	err = AddPost(db, types.RandomNodeID(), post2)
	require.NoError(t, err)
}
