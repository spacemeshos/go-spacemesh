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

var (
	WriteTime     = metrics.NewSimpleCounter(Subsystem, "ballot_write_time_old", "time spent writing ballots")
	WriteTimeHist = metrics.NewHistogramNoLabel(
		"ballot_write_time_hist",
		Subsystem,
		"time spent writing ballots (in seconds)",
		prometheus.ExponentialBuckets(0.01, 10, 5))
)
