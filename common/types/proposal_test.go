package types_test

import (
	"bytes"
	"testing"

	fuzz "github.com/google/gofuzz"
	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-scale/tester"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestProposal_IDSize(t *testing.T) {
	var id types.ProposalID
	require.Len(t, id.Bytes(), types.ProposalIDSize)
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
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	require.NoError(t, p.Initialize())
	require.NotEqual(t, types.EmptyProposalID, p.ID())

	err = p.Initialize()
	require.EqualError(t, err, "proposal already initialized")
}

func FuzzProposalIDConsistency(f *testing.F) {
	tester.FuzzConsistency[types.ProposalID](f)
}

func FuzzProposalIDSafety(f *testing.F) {
	tester.FuzzSafety[types.ProposalID](f)
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
	var proposal types.Proposal
	f := fuzz.NewWithSeed(1001)
	f.Fuzz(&proposal)
	proposal.ActiveSet = nil

	buf := bytes.NewBuffer(nil)
	enc := scale.NewEncoder(buf)
	_, err := proposal.EncodeScale(enc)
	require.NoError(t, err)

	var lid types.LayerID
	_, err = lid.DecodeScale(scale.NewDecoder(buf))
	require.NoError(t, err)
	require.Equal(t, proposal.Layer, lid)
}
