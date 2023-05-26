package timesync

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

var accuracyHist = metrics.NewHistogramWithBuckets(
	"accuracy",
	"clock",
	"positive delta between actual clock notification and expected layer time in ns",
	[]string{},
	prometheus.ExponentialBuckets(10_000, 2, 20),
).WithLabelValues()
