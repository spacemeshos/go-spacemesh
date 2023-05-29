package tortoise

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
	"github.com/stretchr/testify/require"
)

func TestTracer(t *testing.T) {
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
	require.NoError(t, RunTrace(path, WithLogger(logtest.New(t).Named("trace"))))
}
