package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// subsystem shared by all metrics exposed by this package.
	subsystem = "fetch"
)

var (
	totalHits   = metrics.NewCounter("total_hits", subsystem, "Total hash-to-peer cache hits", nil)
	totalMisses = metrics.NewCounter("total_misses", subsystem, "Total hash-to-peer cache misses", nil)
	hitRate     = metrics.NewGauge("hit_rate", subsystem, "Hash-to-peer hit rate", nil)
)

// LogHit logs cache hit.
func LogHit() {
	totalHits.WithLabelValues().Inc()
}

// LogMiss logs cache miss.
func LogMiss() {
	totalMisses.WithLabelValues().Inc()
}

// LogHitRate logs hit rate.
func LogHitRate(hits, misses uint64) {
	rate := float64(hits) / (float64(hits) + float64(misses)) * 100
	hitRate.WithLabelValues().Set(rate)
}
