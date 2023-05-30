package tortoise

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

func TestTracer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tortoise.trace")
	tracer, err := NewTracer(path)
	require.NoError(t, err)

	const size = 12
	s := sim.New(
		sim.WithLayerSize(size),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.WindowSize = 10
	trt := tortoiseFromSimState(t, s.GetState(0), WithConfig(cfg), WithTracer(tracer))
	for i := 0; i < 100; i++ {
		s.Next()
	}
	trt.TallyVotes(ctx, s.Next())
	trt.Updates() // just trace final result
	require.NoError(t, tracer.Close())
	t.Run("live", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, RunTrace(path, WithLogger(logtest.New(t))))
	})
	t.Run("recover", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "tortoise.trace")
		tracer, err := NewTracer(path)
		require.NoError(t, err)
		trt, err := Recover(s.GetState(0).DB, s.GetState(0).Beacons, WithTracer(tracer))
		require.NoError(t, err)
		trt.Updates()
		trt.Results(types.GetEffectiveGenesis(), trt.LatestComplete())
		require.NoError(t, tracer.Close())
		require.NoError(t, RunTrace(path, WithLogger(logtest.New(t))))
	})
	t.Run("errors", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "tortoise.trace")
		tracer, err := NewTracer(path)
		require.NoError(t, err)
		trt, err := New(WithTracer(tracer))
		require.NoError(t, err)
		ballot := &types.Ballot{}
		ballot.Initialize()
		_, err = trt.DecodeBallot(ballot)
		require.Error(t, err)
		require.NoError(t, tracer.Close())
		require.NoError(t, RunTrace(path, WithLogger(logtest.New(t))))
	})
}
