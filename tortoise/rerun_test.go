package tortoise

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
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
		verified = tortoise.HandleIncomingLayer(ctx, last)
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
	verified = tortoise2.HandleIncomingLayer(ctx, last)
	require.Equal(t, last.Sub(1), verified)

	cfg.MeshVerified = last.Sub(1)
	cfg.MeshProcessed = last
	tortoise3 := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	initctx, cancel = context.WithTimeout(ctx, time.Second)
	defer cancel()
	require.NoError(t, tortoise3.WaitReady(initctx))
	verified = tortoise2.HandleIncomingLayer(ctx, last)
	require.Equal(t, last.Sub(1), verified)
}

func TestRerunStaysInVerifyingMode(t *testing.T) {
	ctx := context.Background()
	const (
		size   = 10
		layers = 20
	)
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = cfg.Hdist

	cfg.VerifyingModeVerificationWindow = layers
	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for i := 0; i < layers; i++ {
		last = s.Next()
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(t, last.Sub(1), verified)

	require.NoError(t, tortoise.rerun(ctx))
	verified = tortoise.HandleIncomingLayer(ctx, s.Next())
	require.Equal(t, last, verified)

	require.Equal(t, types.GetEffectiveGenesis(), tortoise.trtl.full.counted)
}

func TestRerunRevertNonverifiedLayers(t *testing.T) {
	ctx := context.Background()
	const (
		size = 10
		good = 10
	)
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

	cfg := defaultTestConfig()
	cfg.LayerSize = size

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(good),
		sim.WithSequence(5, sim.WithVoteGenerator(splitVoting(size))),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	expected := types.GetEffectiveGenesis().Add(good - 1)
	require.Equal(t, expected, verified)

	require.NoError(t, tortoise.rerun(ctx))
	verified = tortoise.HandleIncomingLayer(ctx, s.Next())
	require.True(t, verified.Before(expected))
}

func TestRerunDistanceVoteCounting(t *testing.T) {
	t.Run("FixesMisverified", func(t *testing.T) {
		testWindowCounting(t, 3, 100, 10, true)
	})
	t.Run("ShortWindow", func(t *testing.T) {
		testWindowCounting(t, 3, 100, 3, false)
	})
}

func testWindowCounting(tb testing.TB, maliciousLayers, verifyingWindow, fullWindow int, expectedValidity bool) {
	genesis := types.GetEffectiveGenesis()
	ctx := context.Background()
	const size = 4
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(
		sim.WithSetupMinerRange(size, size),
		sim.WithSetupUnitsRange(2, 2),
	)

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = 1
	cfg.FullModeVerificationWindow = uint32(fullWindow)

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(tb)), WithConfig(cfg))
	var last, verified types.LayerID

	const firstBatch = 2
	misverified := genesis.Add(firstBatch)

	for _, last = range sim.GenLayers(s,
		sim.WithSequence(firstBatch, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		// in this layer voting is malicious. it doesn't support previous layer so there will be no valid blocks in it
		sim.WithSequence(1, sim.WithVoteGenerator(gapVote), sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		sim.WithSequence(maliciousLayers-1, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		// in this layer we skip previously malicious voting.
		sim.WithSequence(1, sim.WithVoteGenerator(skipLayers(maliciousLayers)), sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		sim.WithSequence(10, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
	) {
		verified = tortoise.HandleIncomingLayer(ctx, last)
	}
	require.Equal(tb, last.Sub(1), verified)

	blks, err := blocks.IDsInLayer(s.GetState(0).DB, misverified)
	require.NoError(tb, err)
	for _, blk := range blks {
		validity, err := blocks.IsValid(s.GetState(0).DB, blk)
		require.NoError(tb, err)
		require.False(tb, validity, "validity for block %s", blk)
	}
	require.NoError(tb, tortoise.rerun(ctx))

	for _, blk := range blks {
		validity, err := blocks.IsValid(s.GetState(0).DB, blk)
		require.NoError(tb, err)
		require.Equal(tb, expectedValidity, validity, "validity for block %s layer %s", blk, misverified)
	}
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
