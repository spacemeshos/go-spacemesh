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

var (
	connWaitLatency = metrics.NewHistogramWithBuckets(
		"conn_wait_seconds",
		namespace,
		"time spent in waiting for a connection from a pool",
		[]string{},
		prometheus.ExponentialBuckets(0.01, 2, 20),
	).WithLabelValues()

	queryCacheHits = metrics.NewCounter(
		"querycache_hits",
		namespace,
		"The number of hits in the query cache",
		[]string{"kind", "key"},
	)

	queryCacheMisses = metrics.NewCounter(
		"querycache_misses",
		namespace,
		"The number of misses in the query cache",
		[]string{"kind", "key"},
	)
)
