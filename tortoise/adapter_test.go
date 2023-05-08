package tortoise

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
)

type persistanceAdapter struct {
	testing.TB
	*Tortoise
	db     *datastore.CachedDB
	beacon system.BeaconGetter

	prev types.LayerID
}

func (a *persistanceAdapter) TallyVotes(ctx context.Context, current types.LayerID) {
	genesis := types.GetEffectiveGenesis()
	if a.prev == 0 {
		a.prev = genesis
	}
	for lid := a.prev; lid <= current; lid++ {
		if lid.FirstInEpoch() {
			require.NoError(a, a.db.IterateEpochATXHeaders(lid.GetEpoch(), func(header *types.ActivationTxHeader) bool {
				a.OnAtx(header)
				return true
			}))
			beacon, err := a.beacon.GetBeacon(lid.GetEpoch())
			if err == nil {
				a.OnBeacon(lid.GetEpoch(), beacon)
			}
		}
		blocksrst, err := blocks.Layer(a.db, lid)
		require.NoError(a, err)
		for _, block := range blocksrst {
			valid, _ := blocks.IsValid(a.db, block.ID())
			hare, err := certificates.GetHareOutput(a.db, lid)
			a.OnHistoricalResult(result.Block{
				Header: block.ToVote(),
				Data:   true,
				Valid:  valid,
				Hare:   hare == block.ID(),
			})
			if err == nil {
				a.OnHareOutput(lid, hare)
			}
		}
		ballotsrst, err := ballots.Layer(a.db, lid)
		require.NoError(a, err)
		for _, ballot := range ballotsrst {
			a.OnBallot(ballot)
		}
		coin, err := layers.GetWeakCoin(a.db, lid)
		if err == nil {
			a.OnWeakCoin(lid, coin)
		}
		a.Tortoise.TallyVotes(ctx, lid)
		a.prev = lid
	}
}
