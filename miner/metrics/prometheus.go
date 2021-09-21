package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "miner"
)

// BlockBuildDuration checks block build duration (milliseconds).
var BlockBuildDuration = metrics.NewHistogram(
	"block_build_duration",
	Subsystem,
	"How long it takes to build a block (milliseconds)",
	[]string{
		"layer_id",
		"block_id",
	},
)
