package events_test

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/events"
)

func TestInsertProposalsAndIterate(t *testing.T) {
	db := localsql.InMemoryTest(t)

	var proposals []types.Proposal
	for range 10 {
		p := types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: types.Ballot{
					InnerBallot: types.InnerBallot{
						Layer: types.LayerID(rand.Uint32()),
					},
					SmesherID: types.RandomNodeID(),
				},
				MeshHash: types.RandomHash(),
			},
		}
		require.NoError(t, p.Initialize())
		proposals = append(proposals, p)
		require.NoError(t, events.InsertProposal(db, &p))
	}
	slices.SortFunc(proposals, func(a, b types.Proposal) int { return cmp.Compare(a.Layer, b.Layer) })
	var counter int
	events.IterateAllProposals(db, func(p types.Proposal) bool {
		require.Equal(t, proposals[counter], p)
		counter += 1
		return true
	})
	require.Equal(t, len(proposals), counter)
}
