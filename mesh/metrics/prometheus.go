package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	subsystem = "mesh"
)

// LayerNumBlocks is number of blocks in layer.
var LayerNumBlocks = metrics.NewHistogramWithBuckets(
	"layer_num_blocks",
	subsystem,
	"Number of blocks in layer",
	[]string{},
	prometheus.ExponentialBuckets(1, 2, 16),
)

var BallotWaitWrite = metrics.NewSimpleHistogram(
	"ballot_wait_write",
	subsystem,
	"time waited for ballot to be written",
	prometheus.ExponentialBuckets(0.01, 10, 5),
)

var BallotWaitRetry = metrics.NewSimpleHistogram(
	"ballot_wait_retry",
	subsystem,
	"time waited for when in retry mode",
	prometheus.ExponentialBuckets(0.01, 10, 5),
)
