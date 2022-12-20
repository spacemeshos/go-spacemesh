package types_test

import (
	"testing"

	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())
	require.NotEqual(t, types.EmptyProposalID, p.ID())

	err = p.Initialize()
	require.EqualError(t, err, "proposal already initialized")
}

func TestProposal_Initialize_BadSignature(t *testing.T) {
	p := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *types.RandomBallot(),
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	p.Ballot.Signature = signer.Sign(p.Ballot.SignedBytes())
	p.Signature = signer.Sign(p.Bytes())[1:]
	err = p.Initialize()
	require.EqualError(t, err, "proposal extract nodeId: ed25519: bad signature format")
}

func TestProposal_Initialize_InconsistentBallot(t *testing.T) {
	p := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *types.RandomBallot(),
			TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
		},
	}
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	p.Ballot.Signature = signer1.Sign(p.Ballot.SignedBytes())
	p.Signature = signer2.Sign(p.Bytes())
	err = p.Initialize()
	require.ErrorContains(t, err, "inconsistent smesher in proposal")
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
	types.CheckLayerFirstEncoding(t, func(object types.Proposal) types.LayerID { return object.LayerIndex })
}
