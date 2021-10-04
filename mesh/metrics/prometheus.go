package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "mesh"
)

// LayerNumBlocks is number of blocks in layer.
var LayerNumBlocks = metrics.NewHistogramWithBuckets(
	"layer_num_blocks",
	Subsystem,
	"Number of blocks in layer",
	[]string{},
	prometheus.ExponentialBuckets(1, 2, 16),
)
