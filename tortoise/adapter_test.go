package tortoise

import (
	"context"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/stretchr/testify/require"
)

type persistanceAdapter struct {
	testing.TB
	*Tortoise
	db     *datastore.CachedDB
	beacon system.BeaconGetter

	prev types.LayerID
}

func (a *persistanceAdapter) TallyVotes(ctx context.Context, current types.LayerID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for lid := a.prev; lid <= current; lid++ {
		if lid.FirstInEpoch() {
			require.NoError(a, a.db.IterateEpochATXHeaders(lid.GetEpoch(), func(header *types.ActivationTxHeader) bool {
				a.OnAtx(header)
				return true
			}))
			beacon, err := a.beacon.GetBeacon(lid.GetEpoch())
			require.NoError(a, err)
			a.OnBeacon(lid.GetEpoch(), beacon)
		}
		blocksrst, err := blocks.Layer(a.db, lid)
		require.NoError(a, err)
		for _, block := range blocksrst {
			valid, _ := blocks.IsValid(a.db, block.ID())
			hare, _ := certificates.GetHareOutput(a.db, lid)
			a.OnHistoricalResult(ResultBlock{
				Header: block.ToVote(),
				Data:   true,
				Valid:  valid,
				Hare:   hare == block.ID(),
			})
		}
		ballotsrst, err := ballots.Layer(a.db, lid)
		require.NoError(a, err)
		for _, ballot := range ballotsrst {
			a.OnBallot(ballot)
		}
		coin, err := layers.GetWeakCoin(a.db, lid)
		require.NoError(a, err)
		a.OnWeakCoin(lid, coin)
		a.TallyVotes(ctx, lid)
	}
}
