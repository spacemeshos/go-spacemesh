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

	tortoise := tortoiseFromSimState(t, s.GetState(0), WithLogger(logtest.New(t)), WithConfig(cfg))
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
}

func testWindowCounting(tb testing.TB, maliciousLayers, windowSize int, expectedValidity bool) {
	genesis := types.GetEffectiveGenesis()
	ctx := context.Background()
	const size = 4
	s := sim.New(sim.WithLayerSize(size))
	s.Setup(sim.WithSetupMinerRange(size, size))

	cfg := defaultTestConfig()
	cfg.LayerSize = size
	cfg.Hdist = 1
	cfg.Zdist = 1
	cfg.WindowSize = uint32(windowSize)

	tortoise := tortoiseFromSimState(tb, s.GetState(0), WithLogger(logtest.New(tb)), WithConfig(cfg))

	const firstBatch = 2
	misverified := genesis.Add(firstBatch)

	var last types.LayerID
	for _, last = range sim.GenLayers(s,
		sim.WithSequence(firstBatch, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		// next layer will not vote on the previous layer, layer 9 will be expected to be empty
		sim.WithSequence(1, sim.WithVoteGenerator(skipLayers(1)), sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		// next N-1 layers are not voting for layer 9 as well
		sim.WithSequence(maliciousLayers-1, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		// starting from this layer ballots will support layer 9
		sim.WithSequence(1, sim.WithVoteGenerator(skipLayers(maliciousLayers)), sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
		sim.WithSequence(10, sim.WithEmptyHareOutput(), sim.WithNumBlocks(1)),
	) {
		tortoise.TallyVotes(ctx, last)
		processBlockUpdates(tb, tortoise, s.GetState(0).DB)
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

	tortoise := tortoiseFromSimState(b, s.GetState(0), WithConfig(cfg), WithLogger(logtest.New(b)))

	// generate info
	start := time.Now()
	var last types.LayerID
	for i := 0; i < size; i++ {
		last = s.Next(opts...)
	}
	b.Log("generated state", time.Since(start))
	// count ballots and form initial opinion
	tortoise.TallyVotes(ctx, last)
	b.Log("loaded state", time.Since(start))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// benchmark how long it takes to recheck all layers within the sliding window
		tortoise.TallyVotes(ctx, last)
	}
}
