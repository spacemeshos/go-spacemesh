package sql

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const namespace = "database"

// QueryDuration in nanoseconds.
var queryDuration = metrics.NewHistogramWithBuckets(
	"query_duration",
	namespace,
	"Duration of the query in nanoseconds",
	[]string{"query"},
	prometheus.ExponentialBuckets(100_000, 2, 20),
)
