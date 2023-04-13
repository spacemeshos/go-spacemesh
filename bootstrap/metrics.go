package bootstrap

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "bootstrap"

	// labels for hare consensus output.
	success = "ok"
	failure = "fail"
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

	updateCount = metrics.NewCounter(
		"updates",
		namespace,
		"number of updates received",
		[]string{"outcome"},
	)
	updateOkCount      = updateCount.WithLabelValues(success)
	updateFailureCount = updateCount.WithLabelValues(failure)
)
