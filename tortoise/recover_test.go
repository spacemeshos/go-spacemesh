package tortoise

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/system"
)

type recoveryAdapter struct {
	testing.TB
	*Tortoise
	db     *datastore.CachedDB
	beacon system.BeaconGetter

	prev types.LayerID
}

func (a *recoveryAdapter) TallyVotes(ctx context.Context, current types.LayerID) {
	genesis := types.GetEffectiveGenesis()
	if a.prev == 0 {
		a.prev = genesis
	}
	for lid := a.prev; lid <= current; lid++ {
		require.NoError(a, loadLayer(ctx, a.Tortoise, a.db, a.beacon, lid))
		a.prev = lid
	}
}
