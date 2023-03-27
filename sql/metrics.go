package sql

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const namespace = "database"

func newQueryLatency() *prometheus.HistogramVec {
	return metrics.NewHistogramWithBuckets(
		"query_latency_ns",
		namespace,
		"Latency of the query in nanoseconds",
		[]string{"query"},
		prometheus.ExponentialBuckets(100_000, 2, 20),
	)
}
