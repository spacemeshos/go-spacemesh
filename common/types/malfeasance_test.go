package types_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func TestCodec_MultipleATXs(t *testing.T) {
	nodeID := types.BytesToNodeID([]byte{1, 1, 1})
	lid := types.NewLayerID(11)

	a1 := types.NewActivationTx(types.NIPostChallenge{PubLayerID: lid}, &nodeID, types.Address{1, 2, 3}, nil, 10, nil, nil)
	a2 := types.NewActivationTx(types.NIPostChallenge{PubLayerID: lid}, &nodeID, types.Address{3, 2, 1}, nil, 11, nil, nil)

	var atxProof types.AtxProof
	for i, a := range []*types.ActivationTx{a1, a2} {
		a.SetMetadata()
		copy(a.Signature[:], types.RandomBytes(64))
		atxProof.Messages[i] = types.AtxProofMsg{
			InnerMsg:  a.ATXMetadata,
			Signature: a.Signature,
		}
	}
	proof := &types.MalfeasanceProof{
		Layer: lid,
		Proof: types.Proof{
			Type: types.MultipleATXs,
			Data: &atxProof,
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded types.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func TestCodec_MultipleBallot(t *testing.T) {
	nodeID := types.BytesToNodeID([]byte{1, 1, 1})
	lid := types.NewLayerID(11)

	b1 := types.NewExistingBallot(types.BallotID{1}, [64]byte{}, nodeID, types.BallotMetadata{Layer: lid})
	b2 := types.NewExistingBallot(types.BallotID{2}, [64]byte{}, nodeID, types.BallotMetadata{Layer: lid})

	var ballotProof types.BallotProof
	for i, b := range []types.Ballot{b1, b2} {
		b.SetMetadata()
		copy(b.Signature[:], types.RandomBytes(64))
		ballotProof.Messages[i] = types.BallotProofMsg{
			InnerMsg:  b.BallotMetadata,
			Signature: b.Signature,
		}
	}
	proof := &types.MalfeasanceProof{
		Layer: lid,
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &ballotProof,
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded types.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func TestCodec_HareEquivocation(t *testing.T) {
	lid := types.NewLayerID(11)
	round := uint32(3)

	hm1 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}
	hm2 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}

	var hareProof types.HareProof
	for i, hm := range []types.HareMetadata{hm1, hm2} {
		hareProof.Messages[i] = types.HareProofMsg{
			InnerMsg: hm,
		}
		copy(hareProof.Messages[i].Signature[:], types.RandomBytes(64))
	}
	proof := &types.MalfeasanceProof{
		Layer: lid,
		Proof: types.Proof{
			Type: types.HareEquivocation,
			Data: &hareProof,
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded types.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func TestCodec_MalfeasanceGossip(t *testing.T) {
	lid := types.NewLayerID(11)
	round := uint32(3)

	hm1 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}
	hm2 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}

	var hareProof types.HareProof
	for i, hm := range []types.HareMetadata{hm1, hm2} {
		hareProof.Messages[i] = types.HareProofMsg{
			InnerMsg: hm,
		}
		copy(hareProof.Messages[i].Signature[:], types.RandomBytes(64))
	}
	gossip := &types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: lid,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &hareProof,
			},
		},
	}
	encoded, err := codec.Encode(gossip)
	require.NoError(t, err)

	var decoded types.MalfeasanceGossip
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *gossip, decoded)

	gossip.Eligibility = &types.HareEligibilityGossip{
		Layer:  lid,
		Round:  round,
		PubKey: types.RandomBytes(32),
		Eligibility: types.HareEligibility{
			Proof: []byte{1, 2, 3},
			Count: 12,
		},
	}
	encoded, err = codec.Encode(gossip)
	require.NoError(t, err)

	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *gossip, decoded)
}
