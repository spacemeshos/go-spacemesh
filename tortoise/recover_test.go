package tortoise

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

type recoveryAdapter struct {
	testing.TB
	*Tortoise
	db *datastore.CachedDB

	prev types.LayerID
}

func (a *recoveryAdapter) TallyVotes(ctx context.Context, current types.LayerID) {
	genesis := types.GetEffectiveGenesis()
	if a.prev == 0 {
		a.prev = genesis
	}
	for lid := a.prev; lid <= current; lid++ {
		require.NoError(a, RecoverLayer(ctx, a.Tortoise, a.db, lid, a.OnBallot))
		a.Tortoise.TallyVotes(ctx, lid)
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

	tortoise2, err := Recover(context.Background(), s.GetState(0).DB, last, WithLogger(logtest.New(t)), WithConfig(cfg))
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
	tortoise, err := Recover(context.Background(), s.GetState(0).DB, 100, WithLogger(logtest.New(t)), WithConfig(cfg))
	require.NoError(t, err)
	require.NotNil(t, tortoise)
}

func TestRecoverWithOpinion(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	trt := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
	for _, lid := range sim.GenLayers(s, sim.WithSequence(10)) {
		trt.TallyVotes(context.Background(), lid)
	}
	var last result.Layer
	for _, rst := range trt.Updates() {
		if rst.Verified {
			require.NoError(t, layers.SetMeshHash(s.GetState(0).DB, rst.Layer, rst.Opinion))
		}
		last = rst
	}
	tortoise, err := Recover(
		context.Background(),
		s.GetState(0).DB,
		last.Layer,
		WithLogger(logtest.New(t)),
		WithConfig(cfg),
	)
	require.NoError(t, err)
	require.NotNil(t, tortoise)
	updates := tortoise.Updates()
	require.Len(t, updates, 1)
	require.Equal(t, updates[0], last)
}

func TestResetPending(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	trt := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
	const n = 10
	var last types.LayerID
	for _, lid := range sim.GenLayers(s, sim.WithSequence(n)) {
		last = lid
		trt.TallyVotes(context.Background(), lid)
	}
	updates1 := trt.Updates()
	require.Len(t, updates1, n+1)
	require.Equal(t, types.GetEffectiveGenesis(), updates1[0].Layer)
	require.Equal(t, last, updates1[n].Layer)
	for _, item := range updates1[:n/2] {
		require.NoError(t, layers.SetMeshHash(s.GetState(0).DB, item.Layer, item.Opinion))
	}

	recovered, err := Recover(context.Background(), s.GetState(0).DB, last, WithLogger(logtest.New(t)), WithConfig(cfg))
	require.NoError(t, err)
	updates2 := recovered.Updates()
	require.Len(t, updates2, n/2+1)
	require.Equal(t, last-n/2, updates2[0].Layer)
	require.Equal(t, last, updates2[n/2].Layer)
}

func TestWindowRecovery(t *testing.T) {
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	const epochSize = 4
	require.EqualValues(t, epochSize, types.GetLayersPerEpoch())
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = epochSize*2 + epochSize/2 // to test that window extends to full 3rd epoch

	const n = epochSize * 5
	trt := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(t)))
	var last types.LayerID
	for _, lid := range sim.GenLayers(s, sim.WithSequence(n)) {
		last = lid
		trt.TallyVotes(context.Background(), lid)
	}
	updates1 := trt.Updates()
	require.Len(t, updates1, n+1)
	require.Equal(t, types.GetEffectiveGenesis(), updates1[0].Layer)
	require.Equal(t, last, updates1[n].Layer)
	for _, item := range updates1[:epochSize*4] {
		require.NoError(t, layers.SetMeshHash(s.GetState(0).DB, item.Layer, item.Opinion))
	}

	recovered, err := Recover(context.Background(), s.GetState(0).DB, last, WithLogger(logtest.New(t)), WithConfig(cfg))
	require.NoError(t, err)
	updates2 := recovered.Updates()
	require.Len(t, updates2, epochSize+1)

	for i := range updates1[epochSize*4:] {
		require.Equal(t, updates1[epochSize*4+i].Opinion, updates2[i].Opinion)
	}
}
