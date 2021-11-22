package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "tortoise"
)

// LayerDistanceToBaseBallot checks how far back a node needs to find a good ballot.
var LayerDistanceToBaseBallot = metrics.NewHistogramWithBuckets(
	"layer_distance_to_base_ballot",
	Subsystem,
	"How far back a node needs to find a good ballot",
	[]string{},
	prometheus.ExponentialBuckets(1, 2, 16),
)
