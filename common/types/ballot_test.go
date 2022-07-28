package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestBallot_IDSize(t *testing.T) {
	var id BallotID
	assert.Len(t, id.Bytes(), BallotIDSize)
}

func TestBallot_Initialize(t *testing.T) {
	b := Ballot{
		InnerBallot: InnerBallot{
			AtxID: RandomATXID(),
			Votes: Votes{
				Base:    RandomBallotID(),
				Support: []BlockID{RandomBlockID(), RandomBlockID()},
			},
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
	}
	signer := signing.NewEdSigner()
	b.Signature = signer.Sign(b.Bytes())
	assert.NoError(t, b.Initialize())
	assert.NotEqual(t, EmptyBallotID, b.ID())
	assert.Equal(t, signer.PublicKey(), b.SmesherID())

	err := b.Initialize()
	assert.EqualError(t, err, "ballot already initialized")
}

func TestBallot_Initialize_BadSignature(t *testing.T) {
	b := Ballot{
		InnerBallot: InnerBallot{
			AtxID: RandomATXID(),
			Votes: Votes{
				Base:    RandomBallotID(),
				Support: []BlockID{RandomBlockID(), RandomBlockID()},
			},
			RefBallot:  RandomBallotID(),
			LayerIndex: NewLayerID(10),
		},
	}
	b.Signature = signing.NewEdSigner().Sign(b.Bytes())[1:]
	err := b.Initialize()
	assert.EqualError(t, err, "ballot extract key: ed25519: bad signature format")
}

func TestDBBallot(t *testing.T) {
	layer := NewLayerID(100)
	b := GenLayerBallot(layer)
	assert.Equal(t, layer, b.LayerIndex)
	assert.NotEqual(t, b.ID(), EmptyBallotID)
	assert.NotNil(t, b.SmesherID())
	dbb := &DBBallot{
		InnerBallot: b.InnerBallot,
		ID:          b.ID(),
		Signature:   b.Signature,
		SmesherID:   b.SmesherID().Bytes(),
	}
	got := dbb.ToBallot()
	assert.Equal(t, b, got)
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

func FuzzDBBallotConsistency(f *testing.F) {
	tester.FuzzConsistency[DBBallot](f)
}

func FuzzDBBallotSafety(f *testing.F) {
	tester.FuzzSafety[DBBallot](f)
}
