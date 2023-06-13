package types_test

import (
	"os"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale/tester"
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
	epoch := types.EpochID(11)

	a1 := types.NewActivationTx(types.NIPostChallenge{PublishEpoch: epoch}, types.Address{1, 2, 3}, nil, 10, nil)
	a2 := types.NewActivationTx(types.NIPostChallenge{PublishEpoch: epoch}, types.Address{3, 2, 1}, nil, 11, nil)

	var atxProof types.AtxProof
	for i, a := range []*types.ActivationTx{a1, a2} {
		a.Signature = types.RandomEdSignature()
		a.SmesherID = types.RandomNodeID()
		atxProof.Messages[i] = types.AtxProofMsg{
			InnerMsg: types.ATXMetadata{
				PublishEpoch: a.PublishEpoch,
				MsgHash:      types.BytesToHash(a.HashInnerBytes()),
			},
			SmesherID: a.SmesherID,
			Signature: a.Signature,
		}
	}
	proof := &types.MalfeasanceProof{
		Layer: epoch.FirstLayer(),
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
	lid := types.LayerID(11)

	b1 := types.NewExistingBallot(types.BallotID{1}, types.EmptyEdSignature, nodeID, lid)
	b2 := types.NewExistingBallot(types.BallotID{2}, types.EmptyEdSignature, nodeID, lid)

	var ballotProof types.BallotProof
	for i, b := range []types.Ballot{b1, b2} {
		b.Signature = types.RandomEdSignature()
		ballotProof.Messages[i] = types.BallotProofMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   b.Layer,
				MsgHash: types.BytesToHash(b.HashInnerBytes()),
			},
			SmesherID: b.SmesherID,
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
	lid := types.LayerID(11)
	round := uint32(3)

	hm1 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}
	hm2 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}

	var hareProof types.HareProof
	for i, hm := range []types.HareMetadata{hm1, hm2} {
		hareProof.Messages[i] = types.HareProofMsg{
			InnerMsg:  hm,
			Signature: types.RandomEdSignature(),
		}
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
	lid := types.LayerID(11)
	round := uint32(3)

	hm1 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}
	hm2 := types.HareMetadata{Layer: lid, Round: round, MsgHash: types.RandomHash()}

	var hareProof types.HareProof
	for i, hm := range []types.HareMetadata{hm1, hm2} {
		hareProof.Messages[i] = types.HareProofMsg{
			InnerMsg:  hm,
			Signature: types.RandomEdSignature(),
		}
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
	hm1 := types.HareMetadata{Layer: 1, Round: 1, MsgHash: types.RandomHash()}
	hm2 := types.HareMetadata{Layer: 1, Round: 1, MsgHash: types.RandomHash()}
	require.True(t, hm1.Equivocation(&hm2))

	// Different round -> no equivocation
	hm2 = types.HareMetadata{Layer: 1, Round: 2, MsgHash: types.RandomHash()}
	require.False(t, hm1.Equivocation(&hm2))

	// Different layer -> no equivocation
	hm2 = types.HareMetadata{Layer: 2, Round: 1, MsgHash: types.RandomHash()}
	require.False(t, hm1.Equivocation(&hm2))

	// Same layer and round, same message hash -> no equivocation
	hm2 = types.HareMetadata{Layer: 1, Round: 1, MsgHash: hm1.MsgHash}
	require.False(t, hm1.Equivocation(&hm2))
}

func FuzzProofConsistency(f *testing.F) {
	tester.FuzzConsistency[types.Proof](f, func(p *types.Proof, c fuzz.Continue) {
		switch c.Intn(3) {
		case 0:
			p.Type = types.MultipleATXs
			data := types.AtxProof{}
			c.Fuzz(&data)
			p.Data = &data
		case 1:
			p.Type = types.MultipleBallots
			data := types.BallotProof{}
			c.Fuzz(&data)
			p.Data = &data
		case 2:
			p.Type = types.HareEquivocation
			data := types.HareProof{}
			c.Fuzz(&data)
			p.Data = &data
		}
	})
}

func FuzzProofSafety(f *testing.F) {
	tester.FuzzSafety[types.Proof](f)
}
