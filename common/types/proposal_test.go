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

func TestProposal_IDSize(t *testing.T) {
	var id types.ProposalID
	assert.Len(t, id.Bytes(), types.ProposalIDSize)
}

func TestProposal_Initialize(t *testing.T) {
	p := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *types.RandomBallot(),
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	assert.NoError(t, p.Initialize())
	assert.NotEqual(t, types.EmptyProposalID, p.ID())

	err := p.Initialize()
	assert.EqualError(t, err, "proposal already initialized")
}

func TestProposal_Initialize_BadSignature(t *testing.T) {
	p := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *types.RandomBallot(),
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	signer := signing.NewEdSigner()
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())[1:]
	err := p.Initialize()
	assert.EqualError(t, err, "proposal extract nodeId: ed25519: bad signature format")
}

func TestProposal_Initialize_InconsistentBallot(t *testing.T) {
	p := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *types.RandomBallot(),
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	p.Ballot.Signature = signing.NewEdSigner().Sign(p.Ballot.SignedBytes())
	p.Signature = signing.NewEdSigner().Sign(p.Bytes())
	err := p.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "inconsistent smesher in proposal")
}

func FuzzProposalConsistency(f *testing.F) {
	tester.FuzzConsistency[types.Proposal](f)
}

func FuzzProposalSafety(f *testing.F) {
	tester.FuzzSafety[types.Proposal](f)
}

func FuzzInnerProposalConsistency(f *testing.F) {
	tester.FuzzConsistency[types.InnerProposal](f)
}

func FuzzInnerProposalSafety(f *testing.F) {
	tester.FuzzSafety[types.InnerProposal](f)
}

func TestProposalEncoding(t *testing.T) {
	t.Run("layer is first", func(t *testing.T) {
		proposal := types.Proposal{}
		f := fuzz.NewWithSeed(1001)
		f.Fuzz(&proposal)
		buf, err := codec.Encode(&proposal)
		require.NoError(t, err)
		var lid types.LayerID
		require.NoError(t, codec.Decode(buf, &lid))
		require.Equal(t, proposal.LayerIndex, lid)
	})
}
