package wire_test

import (
	"os"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	awire "github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func TestCodec_MultipleATXs(t *testing.T) {
	epoch := types.EpochID(11)

	a1 := types.NewActivationTx(types.NIPostChallenge{PublishEpoch: epoch}, types.Address{1, 2, 3}, nil, 10, nil)
	a2 := types.NewActivationTx(types.NIPostChallenge{PublishEpoch: epoch}, types.Address{3, 2, 1}, nil, 11, nil)

	var atxProof wire.AtxProof
	for i, a := range []*types.ActivationTx{a1, a2} {
		a.Signature = types.RandomEdSignature()
		a.SmesherID = types.RandomNodeID()
		atxProof.Messages[i] = wire.AtxProofMsg{
			InnerMsg: types.ATXMetadata{
				PublishEpoch: a.PublishEpoch,
				// MsgHash:      types.Hash32(a.ToWireV1().HashInnerBytes()),
			},
			SmesherID: a.SmesherID,
			Signature: a.Signature,
		}
	}
	proof := &wire.MalfeasanceProof{
		Layer: epoch.FirstLayer(),
		Proof: wire.Proof{
			Type: wire.MultipleATXs,
			Data: &atxProof,
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded wire.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func TestCodec_MultipleBallot(t *testing.T) {
	nodeID := types.BytesToNodeID([]byte{1, 1, 1})
	lid := types.LayerID(11)

	b1 := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, nodeID, lid)
	b2 := types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, nodeID, lid)

	var ballotProof wire.BallotProof
	for i, b := range []types.Ballot{b1, b2} {
		b.Signature = types.RandomEdSignature()
		ballotProof.Messages[i] = wire.BallotProofMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   b.Layer,
				MsgHash: types.BytesToHash(b.HashInnerBytes()),
			},
			SmesherID: b.SmesherID,
			Signature: b.Signature,
		}
	}
	proof := &wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.MultipleBallots,
			Data: &ballotProof,
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded wire.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func TestCodec_HareEquivocation(t *testing.T) {
	lid := types.LayerID(11)
	round := uint32(3)

	hm1 := wire.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}
	hm2 := wire.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}

	var hareProof wire.HareProof
	for i, hm := range []wire.HareMetadata{hm1, hm2} {
		hareProof.Messages[i] = wire.HareProofMsg{
			InnerMsg:  hm,
			Signature: types.RandomEdSignature(),
		}
	}
	proof := &wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.HareEquivocation,
			Data: &hareProof,
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded wire.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func TestCodec_MalfeasanceGossip(t *testing.T) {
	lid := types.LayerID(11)
	round := uint32(3)

	hm1 := wire.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}
	hm2 := wire.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}

	var hareProof wire.HareProof
	for i, hm := range []wire.HareMetadata{hm1, hm2} {
		hareProof.Messages[i] = wire.HareProofMsg{
			InnerMsg:  hm,
			Signature: types.RandomEdSignature(),
		}
	}
	gossip := &wire.MalfeasanceGossip{
		MalfeasanceProof: wire.MalfeasanceProof{
			Layer: lid,
			Proof: wire.Proof{
				Type: wire.HareEquivocation,
				Data: &hareProof,
			},
		},
	}
	encoded, err := codec.Encode(gossip)
	require.NoError(t, err)

	var decoded wire.MalfeasanceGossip
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *gossip, decoded)

	gossip.Eligibility = &types.HareEligibilityGossip{
		Layer:  lid,
		Round:  round,
		NodeID: types.RandomNodeID(),
		Eligibility: types.HareEligibility{
			Proof: types.RandomVrfSignature(),
			Count: 12,
		},
	}
	encoded, err = codec.Encode(gossip)
	require.NoError(t, err)

	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *gossip, decoded)
}

func Test_HareMetadata_Equivocation(t *testing.T) {
	// Same layer and round, different message hash -> equivocation
	hm1 := wire.HareMetadata{Layer: 1, Round: 1, MsgHash: types.RandomHash()}
	hm2 := wire.HareMetadata{Layer: 1, Round: 1, MsgHash: types.RandomHash()}
	require.True(t, hm1.Equivocation(&hm2))

	// Different round -> no equivocation
	hm2 = wire.HareMetadata{Layer: 1, Round: 2, MsgHash: types.RandomHash()}
	require.False(t, hm1.Equivocation(&hm2))

	// Different layer -> no equivocation
	hm2 = wire.HareMetadata{Layer: 2, Round: 1, MsgHash: types.RandomHash()}
	require.False(t, hm1.Equivocation(&hm2))

	// Same layer and round, same message hash -> no equivocation
	hm2 = wire.HareMetadata{Layer: 1, Round: 1, MsgHash: hm1.MsgHash}
	require.False(t, hm1.Equivocation(&hm2))
}

func TestCodec_InvalidPostIndex(t *testing.T) {
	lid := types.LayerID(11)
	atx := types.NewActivationTx(
		types.NIPostChallenge{PublishEpoch: lid.GetEpoch()},
		types.Address{1, 2, 3},
		nil, 10, nil,
	)

	proof := &wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.InvalidPostIndex,
			Data: &wire.InvalidPostIndexProof{
				Atx:        *awire.ActivationTxToWireV1(atx),
				InvalidIdx: 5,
			},
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded wire.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func TestCodec_InvalidPrevATX(t *testing.T) {
	lid := types.LayerID(45)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	prev := types.NewActivationTx(
		types.NIPostChallenge{
			PublishEpoch: lid.GetEpoch() - 2,
		},
		types.Address{1, 2, 3},
		nil, 10, nil,
	)
	require.NoError(t, activation.SignAndFinalizeAtx(sig, prev))

	atx1 := types.NewActivationTx(
		types.NIPostChallenge{
			PublishEpoch: lid.GetEpoch() - 1,
			PrevATXID:    prev.ID(),
		},
		types.Address{1, 2, 3},
		nil, 10, nil,
	)
	require.NoError(t, activation.SignAndFinalizeAtx(sig, atx1))

	atx2 := types.NewActivationTx(
		types.NIPostChallenge{
			PublishEpoch: lid.GetEpoch(),
			PrevATXID:    prev.ID(),
		},
		types.Address{1, 2, 3},
		nil, 10, nil,
	)
	require.NoError(t, activation.SignAndFinalizeAtx(sig, atx2))

	proof := &wire.MalfeasanceProof{
		Layer: lid,
		Proof: wire.Proof{
			Type: wire.InvalidPrevATX,
			Data: &wire.InvalidPrevATXProof{
				Atx1: *awire.ActivationTxToWireV1(atx1),
				Atx2: *awire.ActivationTxToWireV1(atx2),
			},
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)

	var decoded wire.MalfeasanceProof
	require.NoError(t, codec.Decode(encoded, &decoded))
	require.Equal(t, *proof, decoded)
}

func FuzzProofConsistency(f *testing.F) {
	tester.FuzzConsistency[wire.Proof](f, func(p *wire.Proof, c fuzz.Continue) {
		switch c.Intn(5) {
		case 0:
			p.Type = wire.MultipleATXs
			data := wire.AtxProof{}
			c.Fuzz(&data)
			p.Data = &data
		case 1:
			p.Type = wire.MultipleBallots
			data := wire.BallotProof{}
			c.Fuzz(&data)
			p.Data = &data
		case 2:
			p.Type = wire.HareEquivocation
			data := wire.HareProof{}
			c.Fuzz(&data)
			p.Data = &data
		case 3:
			p.Type = wire.InvalidPostIndex
			data := wire.InvalidPostIndexProof{}
			c.Fuzz(&data)
			p.Data = &data
		case 4:
			p.Type = wire.InvalidPrevATX
			data := wire.InvalidPrevATXProof{}
			c.Fuzz(&data)
			p.Data = &data
		}
	})
}

func FuzzProofSafety(f *testing.F) {
	tester.FuzzSafety[wire.Proof](f)
}
