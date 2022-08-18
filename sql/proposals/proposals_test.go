package proposals

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/spacemeshos/go-spacemesh/signing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

func TestAdd(t *testing.T) {
	db := sql.InMemory()
	pub := []byte{1, 1}
	ballot := types.NewExistingBallot(types.BallotID{1}, []byte{1, 1}, pub,
		types.InnerBallot{})

	require.NoError(t, ballots.Add(db, &ballot))
	require.NoError(t, identities.SetMalicious(db, pub))
	proposal := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot,
			TxIDs:    []types.TransactionID{{3, 4}},
			MeshHash: types.RandomHash(),
		},
		Signature: []byte{5, 6},
	}
	proposal.SetID(types.ProposalID{7, 8})

	require.NoError(t, Add(db, proposal))
	require.ErrorIs(t, Add(db, proposal), sql.ErrObjectExists)
}

func TestHas(t *testing.T) {
	db := sql.InMemory()
	pub := []byte{1, 1}
	ballot := types.NewExistingBallot(types.BallotID{1}, []byte{1, 1}, pub,
		types.InnerBallot{})

	require.NoError(t, ballots.Add(db, &ballot))
	require.NoError(t, identities.SetMalicious(db, pub))
	proposal := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot,
			TxIDs:    []types.TransactionID{{3, 4}},
			MeshHash: types.RandomHash(),
		},
		Signature: []byte{5, 6},
	}

	proposal.SetID(types.ProposalID{7, 8})

	require.NoError(t, Add(db, proposal))
	has, err := Has(db, proposal.ID())
	require.NoError(t, err)
	require.True(t, has)
}

func TestGet(t *testing.T) {
	db := sql.InMemory()

	pub := []byte{1, 1}
	ballot := types.NewExistingBallot(types.BallotID{1}, []byte{1, 1}, pub,
		types.InnerBallot{})

	require.NoError(t, ballots.Add(db, &ballot))
	require.NoError(t, identities.SetMalicious(db, pub))
	ballot.SetMalicious()

	proposal := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot:   ballot,
			TxIDs:    []types.TransactionID{{3, 4}},
			MeshHash: types.RandomHash(),
		},
		Signature: []byte{5, 6},
	}

	proposal.SetID(types.ProposalID{7, 8})

	require.NoError(t, Add(db, proposal))

	got, err := Get(db, proposal.ID())
	require.NoError(t, err)
	diff := cmp.Diff(proposal, got, cmpopts.EquateEmpty(), cmp.AllowUnexported(types.Ballot{}, types.Proposal{}, signing.PublicKey{}))
	require.Empty(t, diff)
}
