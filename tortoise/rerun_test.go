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

func TestRerunDistanceVoteCounting(t *testing.T) {
	genesis := types.GetEffectiveGenesis()
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup()

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = 1

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID

	const firstBatch = 5
	misverified := genesis.Add(firstBatch)
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(firstBatch, sim.WithEmptyHareOutput()),
		// in this layer voting is malicious, and it will make that misverified layer will be all invalid
		sim.WithSequence(1, sim.WithVoteGenerator(gapVote)),
		// in this layer we skip previously malicious voting
		sim.WithSequence(1, sim.WithVoteGenerator(gapVote)),
		sim.WithSequence(5),
	) {
		_, verified, _ = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	blocks, err := s.GetState(0).MeshDB.LayerBlockIds(misverified)
	require.NoError(t, err)
	for _, block := range blocks {
		validity, err := s.GetState(0).MeshDB.ContextualValidity(block)
		require.NoError(t, err)
		require.False(t, validity, "validity for block %s", block)
	}
	require.NoError(t, tortoise.rerun(ctx))

	for _, block := range blocks {
		validity, err := s.GetState(0).MeshDB.ContextualValidity(block)
		require.NoError(t, err)
		require.True(t, validity, "validity for block %s", block)
	}

	last = s.Next()
	previous, verified, reverted := tortoise.HandleIncomingLayer(ctx, last)
	require.True(t, reverted, "should be reverted")
	require.Equal(t, misverified, previous.Add(1))
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
		benchmarkRerun(b, 10000, 1000, 0)
	})
	b.Run("Full/100", func(b *testing.B) {
		benchmarkRerun(b, 100, 100, 100, sim.WithEmptyHareOutput())
	})
	b.Run("Full/100/Window/10", func(b *testing.B) {
		benchmarkRerun(b, 100, 100, 10, sim.WithEmptyHareOutput())
	})
	b.Run("Full/1000/Window/100", func(b *testing.B) {
		benchmarkRerun(b, 1000, 1000, 100, sim.WithEmptyHareOutput())
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
	cfg.VerifyingModeVerificationWindow = verifyingParam
	cfg.FullModeVerificationWindow = fullParam

	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(b)))
	for i := 0; i < size; i++ {
		tortoise.HandleIncomingLayer(ctx, s.Next(opts...))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tortoise.rerun(ctx)
	}
}
