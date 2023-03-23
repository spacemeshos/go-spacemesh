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
		}
		copy(ballotProof.Messages[i].Signature[:], types.RandomBytes(64))
	}
	proof := &types.MalfeasanceProof{
		Layer: types.NewLayerID(11),
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &ballotProof,
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

func Test_GetMalicious(t *testing.T) {
	db := sql.InMemory()
	got, err := GetMalicious(db)
	require.NoError(t, err)
	require.Nil(t, got)

	const numBad = 11
	bad := make([]types.NodeID, 0, numBad)
	for i := 0; i < numBad; i++ {
		nid := types.NodeID{byte(i + 1)}
		bad = append(bad, nid)
		require.NoError(t, SetMalicious(db, nid, types.RandomBytes(11)))
	}
	got, err = GetMalicious(db)
	require.NoError(t, err)
	require.Equal(t, bad, got)
}
