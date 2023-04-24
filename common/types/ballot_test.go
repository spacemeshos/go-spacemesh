package types_test

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestBallotIDUnaffectedByVotes(t *testing.T) {
	inner := types.InnerBallot{
		Layer: types.LayerID(1),
		AtxID: types.ATXID{1, 2, 3},
	}
	ballot1 := types.Ballot{
		InnerBallot: inner,
	}
	ballot2 := types.Ballot{
		InnerBallot: inner,
	}
	ballot1.Votes.Support = []types.BlockHeader{{ID: types.BlockID{1}}}
	ballot1.Votes.Support = []types.BlockHeader{{ID: types.BlockID{2}}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	ballot1.Signature = signer.Sign(signing.BALLOT, ballot1.SignedBytes())
	ballot2.Signature = signer.Sign(signing.BALLOT, ballot2.SignedBytes())
	ballot1.Initialize()
	ballot2.Initialize()

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
	b.Signature = signer.Sign(signing.BALLOT, b.SignedBytes())
	b.SmesherID = signer.NodeID()
	require.NoError(t, b.Initialize())
	require.NotEqual(t, types.EmptyBallotID, b.ID())

	err = b.Initialize()
	require.EqualError(t, err, "ballot already initialized")
}

func FuzzBallotIDConsistency(f *testing.F) {
	tester.FuzzConsistency[types.BallotID](f)
}

func FuzzBallotIDSafety(f *testing.F) {
	tester.FuzzSafety[types.BallotID](f)
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
