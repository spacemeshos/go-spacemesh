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
		benchmarkRerun(b, 100, 10, 0)
	})
	b.Run("Verifying/1000", func(b *testing.B) {
		benchmarkRerun(b, 1000, 1000, 0)
	})
	b.Run("Verifying/10000", func(b *testing.B) {
		benchmarkRerun(b, 10000, 10000, 0)
	})
	b.Run("Full/100", func(b *testing.B) {
		benchmarkRerun(b, 100, 100, 100, sim.WithEmptyInputVector())
	})
	b.Run("Full/100/Window/10", func(b *testing.B) {
		benchmarkRerun(b, 100, 100, 10, sim.WithEmptyInputVector())
	})
}

func benchmarkRerun(b *testing.B, size int, verifyingParam, fullParam uint32, opts ...sim.NextOpt) {
	const layerSize = 30
	s := sim.New(
		sim.WithLayerSize(layerSize),
		sim.WithPath(b.TempDir()),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = layerSize
	cfg.VerifyingModeRerunWindow = verifyingParam
	cfg.FullModeRerunWindow = fullParam

	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(b)))
	for i := 0; i < size; i++ {
		tortoise.HandleIncomingLayer(ctx, s.Next(opts...))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.rerun(ctx)
	}
}
