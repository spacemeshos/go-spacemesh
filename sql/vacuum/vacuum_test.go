package vacuum

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
)

func TestVacuumDB(t *testing.T) {
	db := sql.InMemory()
	require.NoError(t, VacuumDB(db))
}

func TestFreedPct(t *testing.T) {
	db := sql.InMemory()

	got, err := FreePct(db)
	require.NoError(t, err)
	require.Equal(t, 0, got)

	for i := 0; i < 100; i++ {
		ballot := types.NewExistingBallot(types.RandomBallotID(), types.RandomEdSignature(), types.NodeID{1}, types.LayerID(i))
		proposal := &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: ballot,
			},
			Signature: types.RandomEdSignature(),
		}
		proposal.SetID(types.RandomProposalID())
		require.NoError(t, ballots.Add(db, &ballot))
		require.NoError(t, proposals.Add(db, proposal))
	}

	got, err = FreePct(db)
	require.NoError(t, err)
	require.Equal(t, 0, got)

	require.NoError(t, proposals.Delete(db, 80))
	got, err = FreePct(db)
	require.NoError(t, err)
	require.Greater(t, got, 10)
}
