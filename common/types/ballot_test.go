package types_test

import (
	"testing"

	"github.com/spacemeshos/ed25519-recovery"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type TestKeyExtractor struct{}

func (e TestKeyExtractor) ExtractNodeID(msg, sig []byte) (types.NodeID, error) {
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return types.EmptyNodeID, err
	}
	return types.BytesToNodeID(pub), nil
}

func TestBallotIDUnaffectedByVotes(t *testing.T) {
	meta := types.BallotMetadata{
		Layer: types.NewLayerID(1),
	}
	inner := types.InnerBallot{
		AtxID: types.ATXID{1, 2, 3},
	}
	ballot1 := types.Ballot{
		BallotMetadata: meta,
		InnerBallot:    inner,
	}
	ballot2 := types.Ballot{
		BallotMetadata: meta,
		InnerBallot:    inner,
	}
	ballot1.Votes.Support = []types.Vote{{ID: types.BlockID{1}}}
	ballot1.Votes.Support = []types.Vote{{ID: types.BlockID{2}}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	ballot1.Signature = signer.Sign(ballot1.SignedBytes())
	ballot2.Signature = signer.Sign(ballot2.SignedBytes())
	ballot1.Initialize(&TestKeyExtractor{})
	ballot2.Initialize(&TestKeyExtractor{})

	require.NotEmpty(t, ballot1.ID())
	require.Equal(t, ballot1.ID(), ballot2.ID())
}

func TestBallot_IDSize(t *testing.T) {
	var id types.BallotID
	require.Len(t, id.Bytes(), types.BallotIDSize)
}

func TestBallot_Initialize(t *testing.T) {
	b := types.RandomBallot()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	b.Signature = signer.Sign(b.SignedBytes())
	require.NoError(t, b.Initialize(&TestKeyExtractor{}))
	require.NotEqual(t, types.EmptyBallotID, b.ID())
	require.Equal(t, signer.PublicKey().Bytes(), b.SmesherID().Bytes())

	err = b.Initialize(&TestKeyExtractor{})
	require.EqualError(t, err, "ballot already initialized")
}

func TestBallot_Initialize_BadSignature(t *testing.T) {
	b := types.RandomBallot()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	b.Signature = signer.Sign(b.SignedBytes())[1:]
	err = b.Initialize(&TestKeyExtractor{})
	require.EqualError(t, err, "ballot extract key: ed25519: bad signature format")
}

func TestBallot_Initialize_BadMsgHash(t *testing.T) {
	b := types.RandomBallot()
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	b.Signature = signer.Sign(b.SignedBytes())
	b.MsgHash = types.RandomHash()
	err = b.Initialize(&TestKeyExtractor{})
	require.EqualError(t, err, "bad message hash")
}

func FuzzBallotConsistency(f *testing.F) {
	tester.FuzzConsistency[types.Ballot](f)
}

func FuzzBallotSafety(f *testing.F) {
	tester.FuzzSafety[types.Ballot](f)
}

func FuzzInnerBallotConsistency(f *testing.F) {
	tester.FuzzConsistency[types.InnerBallot](f)
}

func FuzzInnerBallotSafety(f *testing.F) {
	tester.FuzzSafety[types.InnerBallot](f)
}

func FuzzVotesConsistency(f *testing.F) {
	tester.FuzzConsistency[types.Votes](f)
}

func FuzzVotesSafety(f *testing.F) {
	tester.FuzzSafety[types.Votes](f)
}

func FuzzEpochDataConsistency(f *testing.F) {
	tester.FuzzConsistency[types.EpochData](f)
}

func FuzzEpochDataSafety(f *testing.F) {
	tester.FuzzSafety[types.EpochData](f)
}

func FuzzVotingEligibilityProofConsistency(f *testing.F) {
	tester.FuzzConsistency[types.VotingEligibility](f)
}

func FuzzVotingEligibilityProofSafety(f *testing.F) {
	tester.FuzzSafety[types.VotingEligibility](f)
}

func TestBallotEncoding(t *testing.T) {
	types.CheckLayerFirstEncoding(t, func(object types.Ballot) types.LayerID { return object.Layer })
}
