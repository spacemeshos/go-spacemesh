package types

import (
	"testing"

	"github.com/spacemeshos/poet/shared"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
)

// Regression test for https://github.com/spacemeshos/go-spacemesh/issues/4386
func Test_Encode_Decode_ProofMessage(t *testing.T) {
	proofMessage := &PoetProofMessage{
		PoetProof: PoetProof{
			MerkleProof: shared.MerkleProof{
				Root: []byte{1, 2, 3},
			},
			LeafCount: 1234,
		},
		PoetServiceID: []byte("poet_id_123456"),
		RoundID:       "1337",
	}
	encoded, err := codec.Encode(proofMessage)
	require.NoError(t, err)
	var pf PoetProofMessage
	err = codec.Decode(encoded, &pf)
	require.NoError(t, err)
}
