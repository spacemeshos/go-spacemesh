package bootstrap

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "bootstrap"

	success = "ok"
	failure = "fail"

	labelQuery = "query"
)

var (
	queryCount = metrics.NewCounter(
		"query",
		namespace,
		"number of queries made to the update site",
		[]string{"outcome"},
	)
	queryOkCount      = queryCount.WithLabelValues(success)
	queryFailureCount = queryCount.WithLabelValues(failure)

	received = metrics.NewCounter(
		"received",
		namespace,
		"total bytes received",
		nil,
	).WithLabelValues()

	updateCount = metrics.NewCounter(
		"updates",
		namespace,
		"number of updates received",
		[]string{"outcome"},
	)
	updateOkCount      = updateCount.WithLabelValues(success)
	updateFailureCount = updateCount.WithLabelValues(failure)

	queryDuration = metrics.NewHistogramWithBuckets(
		"query_duration",
		namespace,
		"duration in ns for querying update",
		[]string{"step"},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	)
)
