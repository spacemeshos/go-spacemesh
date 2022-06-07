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

// HashToPeersCacheCollector implement metrics.Reporter
// that keeps track of the number of messages sent and received per protocol.
type HashToPeersCacheCollector struct{}

// NewHashToPeersCacheCollector creates a new HashToPeersCacheCollector.
func NewHashToPeersCacheCollector() *HashToPeersCacheCollector {
	return &HashToPeersCacheCollector{}
}

// LogHit logs cache hit.
func (b *HashToPeersCacheCollector) LogHit() {
	totalHits.WithLabelValues().Inc()
}

// LogMiss logs the message received from the peer.
func (b *HashToPeersCacheCollector) LogMiss() {
	totalMisses.WithLabelValues().Inc()
}
