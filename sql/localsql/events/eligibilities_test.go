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

func TestInsertEligibilitiesAndIterate(t *testing.T) {
	db := localsql.InMemoryTest(t)

	type eligibility struct {
		types.VotingEligibility
		id    types.NodeID
		layer types.LayerID
	}

	var eligibilities []eligibility
	for range 10 {
		e := eligibility{
			layer: types.LayerID(rand.Uint32()),
			id:    types.RandomNodeID(),
			VotingEligibility: types.VotingEligibility{
				J:   rand.Uint32(),
				Sig: types.RandomVrfSignature(),
			},
		}
		eligibilities = append(eligibilities, e)
		require.NoError(t, events.InsertEligibility(db, e.id, e.layer, &e.VotingEligibility))
	}

	slices.SortFunc(eligibilities, func(a, b eligibility) int { return cmp.Compare(a.layer, b.layer) })
	var counter int
	events.IterateAllEligibilities(db, func(id types.NodeID, layer types.LayerID, e *types.VotingEligibility) bool {
		got := eligibility{
			VotingEligibility: *e,
			id:                id,
			layer:             layer,
		}
		require.Equal(t, eligibilities[counter], got)
		counter += 1
		return true
	})
	require.Equal(t, len(eligibilities), counter)
}
