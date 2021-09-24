package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "tortoise"
)

// Metrics labels.
const (
	LastLayerLabel = "last_layer"
	BlockIDLabel   = "block_id"
)

// LayerDistanceToBaseBlock checks how far back a node needs to find a good block.
var LayerDistanceToBaseBlock = metrics.NewHistogramWithBuckets(
	"layer_distance_to_base_block",
	Subsystem,
	"How far back a node needs to find a good block",
	[]string{
		LastLayerLabel,
	},
	prometheus.ExponentialBuckets(1, 2, 16),
)
