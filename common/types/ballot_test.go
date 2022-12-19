package types_test

import (
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestBallotIDUnaffectedByVotes(t *testing.T) {
	inner := types.InnerBallot{
		LayerIndex: types.NewLayerID(1),
	}
	ballot1 := types.Ballot{
		InnerBallot: inner,
	}
	ballot2 := types.Ballot{
		InnerBallot: inner,
	}
	ballot1.Votes.Support = []types.Vote{{ID: types.BlockID{1}}}
	ballot1.Votes.Support = []types.Vote{{ID: types.BlockID{2}}}
	signer := signing.NewEdSigner()
	ballot1.Signature = signer.Sign(ballot1.SignedBytes())
	ballot2.Signature = signer.Sign(ballot2.SignedBytes())
	ballot1.Initialize()
	ballot2.Initialize()

	require.NotEmpty(t, ballot1.ID())
	require.Equal(t, ballot1.ID(), ballot2.ID())
}

func TestBallot_IDSize(t *testing.T) {
	var id types.BallotID
	assert.Len(t, id.Bytes(), types.BallotIDSize)
}

func TestBallot_Initialize(t *testing.T) {
	b := types.Ballot{
		InnerBallot: types.InnerBallot{
			AtxID:      types.RandomATXID(),
			RefBallot:  types.RandomBallotID(),
			LayerIndex: types.NewLayerID(10),
		},
		Votes: types.Votes{
			Base:    types.RandomBallotID(),
			Against: []types.Vote{{ID: types.RandomBlockID()}, {ID: types.RandomBlockID()}},
		},
	}
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.SignedBytes())
	assert.NoError(t, b.Initialize())
	assert.NotEqual(t, types.EmptyBallotID, b.ID())
	assert.Equal(t, signer.PublicKey().Bytes(), b.SmesherID().Bytes())

	err := b.Initialize()
	assert.EqualError(t, err, "ballot already initialized")
}

func TestBallot_Initialize_BadSignature(t *testing.T) {
	b := types.Ballot{
		InnerBallot: types.InnerBallot{
			AtxID:      types.RandomATXID(),
			RefBallot:  types.RandomBallotID(),
			LayerIndex: types.NewLayerID(10),
		},
		Votes: types.Votes{
			Base:    types.RandomBallotID(),
			Support: []types.Vote{{ID: types.RandomBlockID()}, {ID: types.RandomBlockID()}},
		},
	}
	b.Signature = signing.NewEdSigner().Sign(b.SignedBytes())[1:]
	err := b.Initialize()
	assert.EqualError(t, err, "ballot extract key: ed25519: bad signature format")
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
	tester.FuzzConsistency[types.VotingEligibilityProof](f)
}

func FuzzVotingEligibilityProofSafety(f *testing.F) {
	tester.FuzzSafety[types.VotingEligibilityProof](f)
}

func TestBallotEncoding(t *testing.T) {
	t.Run("layer is first", func(t *testing.T) {
		ballot := types.Ballot{}
		f := fuzz.NewWithSeed(1001)
		f.Fuzz(&ballot)
		buf, err := codec.Encode(&ballot)
		require.NoError(t, err)
		var lid types.LayerID
		require.NoError(t, codec.Decode(buf, &lid))
		require.Equal(t, ballot.LayerIndex, lid)
	})
}
