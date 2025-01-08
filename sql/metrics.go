package sql

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "database"
	dbLabel   = "db"
)

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
	ConnWaitLatency = metrics.NewHistogramWithBuckets(
		"conn_wait_seconds",
		namespace,
		"time spent in waiting for a connection from a pool",
		[]string{dbLabel},
		prometheus.ExponentialBuckets(0.01, 2, 20),
	)

	PoolUsage = metrics.NewGauge(
		"pool_usage",
		namespace,
		"number of connections in use",
		[]string{dbLabel},
	)
)
