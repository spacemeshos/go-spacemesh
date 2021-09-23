package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "miner"
)

// Metrics labels.
const (
	LayerIDLabel = "layer_id"
	BlockIDLabel = "block_id"
)

// BlockBuildDuration checks block build duration (milliseconds).
var BlockBuildDuration = metrics.NewHistogramWithBuckets(
	"block_build_duration",
	Subsystem,
	"How long it takes to build a block (milliseconds)",
	[]string{
		LayerIDLabel,
		BlockIDLabel,
	},
	[]float64{10, 100, 1000, 5 * 1000, 10 * 1000, 60 * 1000, 10 * 60 * 1000, 60 * 60 * 1000},
)
