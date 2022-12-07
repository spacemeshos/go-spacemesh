package types

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestProposal_IDSize(t *testing.T) {
	var id ProposalID
	assert.Len(t, id.Bytes(), ProposalIDSize)
}

func TestProposal_Initialize(t *testing.T) {
	p := Proposal{
		InnerProposal: InnerProposal{
			Ballot: *RandomBallot(),
			TxIDs:  []TransactionID{RandomTransactionID(), RandomTransactionID()},
		},
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	assert.NoError(t, p.Initialize())
	assert.NotEqual(t, EmptyProposalID, p.ID())

	err := p.Initialize()
	assert.EqualError(t, err, "proposal already initialized")
}

func TestProposal_Initialize_BadSignature(t *testing.T) {
	p := Proposal{
		InnerProposal: InnerProposal{
			Ballot: *RandomBallot(),
			TxIDs:  []TransactionID{RandomTransactionID(), RandomTransactionID()},
		},
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())[1:]
	err := p.Initialize()
	assert.EqualError(t, err, "proposal extract key: ed25519: bad signature format")
}

func TestProposal_Initialize_InconsistentBallot(t *testing.T) {
	p := Proposal{
		InnerProposal: InnerProposal{
			Ballot: *RandomBallot(),
			TxIDs:  []TransactionID{RandomTransactionID(), RandomTransactionID()},
		},
	}
	p.Ballot.Signature = signing.NewEdSigner().Sign(p.Ballot.SignedBytes())
	p.Signature = signing.NewEdSigner().Sign(p.Bytes())
	err := p.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inconsistent smesher in proposal")
}

func FuzzProposalConsistency(f *testing.F) {
	tester.FuzzConsistency[Proposal](f)
}

func FuzzProposalSafety(f *testing.F) {
	tester.FuzzSafety[Proposal](f)
}

func FuzzInnerProposalConsistency(f *testing.F) {
	tester.FuzzConsistency[InnerProposal](f)
}

func FuzzInnerProposalSafety(f *testing.F) {
	tester.FuzzSafety[InnerProposal](f)
}

func TestProposalEncoding(t *testing.T) {
	t.Run("layer is first", func(t *testing.T) {
		proposal := Proposal{}
		lid := layerTester(t, &proposal)
		require.Equal(t, proposal.LayerIndex, lid)
	})
}
