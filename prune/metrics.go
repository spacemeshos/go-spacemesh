package prune

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const namespace = "prune"

var (
	pruneLatency = metrics.NewHistogramWithBuckets(
		"prune_seconds",
		namespace,
		"prune time in seconds",
		[]string{"step"},
		prometheus.ExponentialBuckets(0.01, 2, 10),
	)
	proposalLatency  = pruneLatency.WithLabelValues("proposal")
	certLatency      = pruneLatency.WithLabelValues("cert")
	propTxLatency    = pruneLatency.WithLabelValues("proptxs")
	activeSetLatency = pruneLatency.WithLabelValues("activeset")
)
