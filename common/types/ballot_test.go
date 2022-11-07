package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestBallotIDUnaffectedByVotes(t *testing.T) {
	inner := InnerBallot{
		LayerIndex: NewLayerID(1),
	}
	ballot1 := Ballot{
		InnerBallot: inner,
	}
	ballot2 := Ballot{
		InnerBallot: inner,
	}
	ballot1.Votes.Support = []Vote{{ID: BlockID{1}}}
	ballot1.Votes.Support = []Vote{{ID: BlockID{2}}}
	signer := signing.NewEdSigner()
	ballot1.Signature = signer.Sign(ballot1.SignedBytes())
	ballot2.Signature = signer.Sign(ballot2.SignedBytes())
	ballot1.Initialize()
	ballot2.Initialize()

	require.NotEmpty(t, ballot1.ID())
	require.Equal(t, ballot1.ID(), ballot2.ID())
}

func TestBallot_IDSize(t *testing.T) {
	var id BallotID
	assert.Len(t, id.Bytes(), BallotIDSize)
}

func TestBallot_Initialize(t *testing.T) {
	b := Ballot{
		InnerBallot: InnerBallot{
			AtxID:      RandomATXID(),
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
		Votes: Votes{
			Base:    RandomBallotID(),
			Against: []Vote{{ID: RandomBlockID()}, {ID: RandomBlockID()}},
		},
	}
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.SignedBytes())
	assert.NoError(t, b.Initialize())
	assert.NotEqual(t, EmptyBallotID, b.ID())
	assert.Equal(t, signer.PublicKey(), b.SmesherID())

	err := b.Initialize()
	assert.EqualError(t, err, "ballot already initialized")
}

func TestBallot_Initialize_BadSignature(t *testing.T) {
	b := Ballot{
		InnerBallot: InnerBallot{
			AtxID:      RandomATXID(),
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
		Votes: Votes{
			Base:    RandomBallotID(),
			Support: []Vote{{ID: RandomBlockID()}, {ID: RandomBlockID()}},
		},
	}
	b.Signature = signing.NewEdSigner().Sign(b.SignedBytes())[1:]
	err := b.Initialize()
	assert.EqualError(t, err, "ballot extract key: ed25519: bad signature format")
}

func FuzzBallotConsistency(f *testing.F) {
	tester.FuzzConsistency[Ballot](f)
}

func FuzzBallotSafety(f *testing.F) {
	tester.FuzzSafety[Ballot](f)
}

func FuzzInnerBallotConsistency(f *testing.F) {
	tester.FuzzConsistency[InnerBallot](f)
}

func FuzzInnerBallotSafety(f *testing.F) {
	tester.FuzzSafety[InnerBallot](f)
}

func FuzzVotesConsistency(f *testing.F) {
	tester.FuzzConsistency[Votes](f)
}

func FuzzVotesSafety(f *testing.F) {
	tester.FuzzSafety[Votes](f)
}

func FuzzEpochDataConsistency(f *testing.F) {
	tester.FuzzConsistency[EpochData](f)
}

func FuzzEpochDataSafety(f *testing.F) {
	tester.FuzzSafety[EpochData](f)
}

func FuzzVotingEligibilityProofConsistency(f *testing.F) {
	tester.FuzzConsistency[VotingEligibilityProof](f)
}

func FuzzVotingEligibilityProofSafety(f *testing.F) {
	tester.FuzzSafety[VotingEligibilityProof](f)
}
