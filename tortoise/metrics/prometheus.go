package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "tortoise"
)

var LayerDistanceToBaseBlock = metrics.NewHistogram(
	"layer_distance_to_base_block",
	Subsystem,
	"How far back a node needs to find a good block",
	[]string{
		"last_layer",
		"last_evicted",
		"base_block_layer",
		"block_id",
	},
)
