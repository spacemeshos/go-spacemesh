package tortoise

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
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
		require.NoError(a, RecoverLayer(ctx, a.Tortoise, a.db, a.beacon, lid))
		a.prev = lid
	}
}

func TestRecoverState(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(t, s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for i := 0; i < 50; i++ {
		last = s.Next()
		tortoise.TallyVotes(ctx, last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)

	tortoise2, err := Recover(s.GetState(0).DB, s.GetState(0).Beacons, WithLogger(logtest.New(t)), WithConfig(cfg))
	require.NoError(t, err)
	verified = tortoise2.LatestComplete()
	require.Equal(t, last.Sub(1), verified)
	tortoiseFromSimState(t, s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	tortoise2.TallyVotes(ctx, last)
	verified = tortoise2.LatestComplete()
	require.Equal(t, last.Sub(1), verified)
}

func TestRecoverEmpty(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise, err := Recover(s.GetState(0).DB, s.GetState(0).Beacons, WithLogger(logtest.New(t)), WithConfig(cfg))
	require.NoError(t, err)
	require.NotNil(t, tortoise)
}
