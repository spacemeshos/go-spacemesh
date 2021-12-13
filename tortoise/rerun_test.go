package tortoise

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/tortoise/sim"
)

func TestRecoverState(t *testing.T) {
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for i := 0; i < 50; i++ {
		last = s.Next()
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	cfg.MeshVerified = verified
	cfg.MeshProcessed = last
	tortoise2 := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	initctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for i := 0; i < 5; i++ {
		// test that it won't block on multiple attempts
		require.NoError(t, tortoise2.WaitReady(initctx))
	}
	_, verified, _ = tortoise2.HandleIncomingLayer(ctx, last)
	require.Equal(t, last.Sub(1), verified)

	cfg.MeshVerified = last.Sub(1)
	cfg.MeshProcessed = last
	tortoise3 := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	initctx, cancel = context.WithTimeout(ctx, time.Second)
	defer cancel()
	require.NoError(t, tortoise3.WaitReady(initctx))
	old, verified, _ := tortoise2.HandleIncomingLayer(ctx, last)
	require.Equal(t, cfg.MeshVerified, old)
	require.Equal(t, last.Sub(1), verified)
}

func BenchmarkRerun(b *testing.B) {
	b.Run("Verifying/100", func(b *testing.B) {
		benchmarkRerun(b, 100, 100)
	})
	b.Run("Full/100", func(b *testing.B) {
		benchmarkRerun(b, 100, 1, sim.WithEmptyInputVector())
	})
}

func benchmarkRerun(b *testing.B, size, confidence int, opts ...sim.NextOpt) {
	const layerSize = 30
	s := sim.New(
		sim.WithLayerSize(layerSize),
		sim.WithPath(b.TempDir()),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = layerSize
	cfg.ConfidenceParam = uint32(confidence)

	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg))
	for i := 0; i < size; i++ {
		tortoise.HandleIncomingLayer(ctx, s.Next())
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.rerun(ctx)
	}
}
