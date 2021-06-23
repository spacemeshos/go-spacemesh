package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "consensus"
)

var (
	pbaseCount = metrics.NewGauge(
		"pbase_counter",
		MetricsSubsystem,
		"pbase index",
		nil,
	)

	processedCount = metrics.NewGauge(
		"processed_index",
		MetricsSubsystem,
		"Number of layers processed",
		nil,
	)

	blockVotes = metrics.NewGauge(
		"block_votes",
		MetricsSubsystem,
		"Block validity",
		[]string{"validity"},
	)
	validBlocks   = blockVotes.With("validity", "valid")
	invalidBlocks = blockVotes.With("validity", "invalid")
)
