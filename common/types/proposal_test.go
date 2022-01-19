package types

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
	p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
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
	p.Ballot.Signature = signer.Sign(p.Ballot.Bytes())
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
	p.Ballot.Signature = signing.NewEdSigner().Sign(p.Ballot.Bytes())
	p.Signature = signing.NewEdSigner().Sign(p.Bytes())
	err := p.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inconsistent smesher in proposal")
}

func TestDBProposal(t *testing.T) {
	layer := NewLayerID(100)
	p := GenLayerProposal(layer, RandomTXSet(199))
	assert.Equal(t, layer, p.LayerIndex)
	assert.NotEqual(t, p.ID(), EmptyProposalID)
	assert.NotNil(t, p.SmesherID())
	dbb := &DBProposal{
		ID:         p.ID(),
		BallotID:   p.Ballot.ID(),
		LayerIndex: p.LayerIndex,
		TxIDs:      p.TxIDs,
		Signature:  p.Signature,
	}
	got := dbb.ToProposal(&p.Ballot)
	assert.Equal(t, p, got)
}
