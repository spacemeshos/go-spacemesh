package nipost

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func Test_AddNIPost(t *testing.T) {
	db := localsql.InMemoryTest(t)

	nodeID := types.RandomNodeID()
	refNipost := &NIPostState{
		NIPost: &types.NIPost{
			Post: &types.Post{
				Nonce:   1,
				Indices: []byte{1, 2, 3},
				Pow:     1,
			},
			Membership: types.MerkleProof{
				Nodes:     []types.Hash32{types.RandomHash()},
				LeafIndex: 1,
			},
			PostMetadata: &types.PostMetadata{
				Challenge:     types.RandomHash().Bytes(),
				LabelsPerUnit: 1,
			},
		},
		NumUnits: 1,
		VRFNonce: types.VRFPostIndex(1),
	}

	err := AddNIPost(db, nodeID, refNipost)
	require.NoError(t, err)

	nipost, err := NIPost(db, nodeID)
	require.NoError(t, err)
	require.Equal(t, refNipost, nipost)

	err = RemoveNIPost(db, nodeID)
	require.NoError(t, err)

	nipost, err = NIPost(db, nodeID)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, nipost)
}

func Test_AddNIPost_NoDuplicates(t *testing.T) {
	db := localsql.InMemoryTest(t)

	refNipost := &NIPostState{
		NIPost: &types.NIPost{
			Post: &types.Post{
				Nonce:   1,
				Indices: []byte{1, 2, 3},
				Pow:     1,
			},
			Membership: types.MerkleProof{
				Nodes:     []types.Hash32{types.RandomHash(), types.RandomHash()},
				LeafIndex: 1,
			},
			PostMetadata: &types.PostMetadata{
				Challenge:     types.RandomHash().Bytes(),
				LabelsPerUnit: 1,
			},
		},
		NumUnits: 1,
		VRFNonce: types.VRFPostIndex(1),
	}
	refNipost2 := &NIPostState{
		NIPost: &types.NIPost{
			Post: &types.Post{
				Nonce:   2,
				Indices: []byte{1, 2, 3},
				Pow:     1,
			},
			Membership: types.MerkleProof{
				Nodes:     []types.Hash32{types.RandomHash(), types.RandomHash()},
				LeafIndex: 1,
			},
			PostMetadata: &types.PostMetadata{
				Challenge:     types.RandomHash().Bytes(),
				LabelsPerUnit: 1,
			},
		},
		NumUnits: 2,
		VRFNonce: types.VRFPostIndex(2),
	}

	nodeID := types.RandomNodeID()
	err := AddNIPost(db, nodeID, refNipost)
	require.NoError(t, err)

	// fail to add challenge for same node
	err = AddNIPost(db, nodeID, refNipost2)
	require.ErrorIs(t, err, sql.ErrObjectExists)

	// succeed to add challenge for different node
	err = AddNIPost(db, types.RandomNodeID(), refNipost2)
	require.NoError(t, err)
}
