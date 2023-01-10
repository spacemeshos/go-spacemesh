package identities

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestMalicious(t *testing.T) {
	db := sql.InMemory()

	nodeID := types.NodeID{1, 1, 1, 1}
	mal, err := IsMalicious(db, nodeID)
	require.NoError(t, err)
	require.False(t, mal)

	var ballotProof types.BallotProof
	for i := 0; i < 2; i++ {
		ballotProof.Messages[i] = types.BallotProofMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   types.NewLayerID(9),
				MsgHash: types.RandomHash(),
			},
			Signature: types.RandomBytes(64),
		}
	}
	proof := &types.MalfeasanceProof{
		Layer: types.NewLayerID(11),
		ProofData: types.TypedProof{
			Type:  types.MultipleBallots,
			Proof: &ballotProof,
		},
	}
	data, err := codec.Encode(proof)
	require.NoError(t, err)
	require.NoError(t, SetMalicious(db, nodeID, data))

	mal, err = IsMalicious(db, nodeID)
	require.NoError(t, err)
	require.True(t, mal)

	got, err := GetMalfeasanceProof(db, nodeID)
	require.NoError(t, err)
	require.EqualValues(t, proof, got)
}
