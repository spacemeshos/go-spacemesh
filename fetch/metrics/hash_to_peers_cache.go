package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// subsystem shared by all metrics exposed by this package.
	subsystem = "fetch"
)

var (
	totalHits   = metrics.NewCounter("total_hits", subsystem, "Total cache hits", nil)
	totalMisses = metrics.NewCounter("total_misses", subsystem, "Total cache misses", nil)
)

// LogHit logs cache hit.
func LogHit() {
	totalHits.WithLabelValues().Inc()
}

// LogMiss logs the message received from the peer.
func LogMiss() {
	totalMisses.WithLabelValues().Inc()
}
