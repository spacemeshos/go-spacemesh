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
		tortoise.TallyVotes(ctx, last)
		verified = tortoise.LatestComplete()
	}
	require.Equal(t, last.Sub(1), verified)

	tortoise2 := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	initctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for i := 0; i < 5; i++ {
		// test that it won't block on multiple attempts
		require.NoError(t, tortoise2.WaitReady(initctx))
	}
	tortoise2.TallyVotes(ctx, last)
	verified = tortoise2.LatestComplete()
	require.Equal(t, last.Sub(1), verified)

	tortoise3 := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	initctx, cancel = context.WithTimeout(ctx, time.Second)
	defer cancel()
	require.NoError(t, tortoise3.WaitReady(initctx))
	tortoise2.TallyVotes(ctx, last)
	verified = tortoise2.LatestComplete()
	require.Equal(t, last.Sub(1), verified)
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
	cfg.Hdist = good + 1
	cfg.LayerSize = size

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
	var last, verified types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(good),
		sim.WithSequence(5, sim.WithVoteGenerator(splitVoting(size))),
	) {
		tortoise.TallyVotes(ctx, last)
		verified = tortoise.LatestComplete()
	}
	expected := types.GetEffectiveGenesis().Add(good - 1)
	require.True(t, verified.Before(expected))
}

func TestWindowSizeVoteCounting(t *testing.T) {
	t.Run("FixesMisverified", func(t *testing.T) {
		testWindowCounting(t, 3, 10, true)
	})
	t.Run("ShortWindow", func(t *testing.T) {
		testWindowCounting(t, 3, 3, false)
	})
}

func testWindowCounting(tb testing.TB, maliciousLayers, windowSize int, expectedValidity bool) {
	genesis := types.GetEffectiveGenesis()
	ctx := context.Background()
	const size = 10
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupUnitsRange(2, 2))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = 1
	cfg.WindowSize = uint32(windowSize)

	tortoise := tortoiseFromSimState(s.GetState(0), WithLogger(logtest.New(tb)), WithConfig(cfg))

	const firstBatch = 2
	misverified := genesis.Add(firstBatch)

	var last types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(firstBatch, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		// in this layer voting is malicious. it doesn't support previous layer so there will be no valid blocks in it
		sim.WithSequence(1, sim.WithVoteGenerator(gapVote), sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		sim.WithSequence(maliciousLayers-1, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		// in this layer we skip previously malicious voting.
		sim.WithSequence(1, sim.WithVoteGenerator(skipLayers(maliciousLayers)), sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		sim.WithSequence(10, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
	) {
		tortoise.TallyVotes(ctx, last)
	}
	require.Equal(tb, last.Sub(1), tortoise.LatestComplete())

	blks, err := blocks.IDsInLayer(s.GetState(0).DB, misverified)
	require.NoError(tb, err)

	for _, blk := range blks {
		validity, err := blocks.IsValid(s.GetState(0).DB, blk)
		require.NoError(tb, err, "layer %s", misverified)
		require.Equal(tb, expectedValidity, validity, "validity for block %s layer %s", blk, misverified)
	}
}

func BenchmarkTallyVotes(b *testing.B) {
	b.Run("Verifying/100", func(b *testing.B) {
		benchmarkTallyVotes(b, 100, 100)
	})
	b.Run("Verifying/1000", func(b *testing.B) {
		benchmarkTallyVotes(b, 1000, 1000)
	})
	b.Run("Verifying/10000", func(b *testing.B) {
		benchmarkTallyVotes(b, 10000, 10000)
	})
	b.Run("Full/100/Window/10", func(b *testing.B) {
		benchmarkTallyVotes(b, 100, 10, sim.WithEmptyHareOutput())
	})
	b.Run("Full/1000/Window/100", func(b *testing.B) {
		benchmarkTallyVotes(b, 1000, 100, sim.WithEmptyHareOutput())
	})
	b.Run("Full/1000/Window/1000", func(b *testing.B) {
		benchmarkTallyVotes(b, 1000, 1000, sim.WithEmptyHareOutput())
	})
	b.Run("Full/2000/Window/2000", func(b *testing.B) {
		benchmarkTallyVotes(b, 2000, 2000, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1))
	})
	b.Run("Full/10000/Window/10000", func(b *testing.B) {
		benchmarkTallyVotes(b, 10000, 10000, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1))
	})
}

func benchmarkTallyVotes(b *testing.B, size int, windowsize uint32, opts ...sim.NextOpt) {
	const layerSize = 30
	s := sim.New(
		sim.WithLayerSize(layerSize),
		sim.WithPath(b.TempDir()),
	)
	s.Setup()

	ctx := context.Background()
	cfg := defaultTestConfig()
	cfg.LayerSize = layerSize
	cfg.WindowSize = windowsize

	tortoise := tortoiseFromSimState(s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(b)))

	// generate info
	start := time.Now()
	var last types.LayerID
	for i := 0; i < size; i++ {
		last = s.Next(opts...)
	}
	b.Log("generated state", time.Since(start))
	// count ballots and form initial opinion
	tortoise.TallyVotes(ctx, last)
	b.Log("counted votes", time.Since(start))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// benchmark how long it takes to recheck all layers within the sliding window
		tortoise.TallyVotes(ctx, last)
	}
}
