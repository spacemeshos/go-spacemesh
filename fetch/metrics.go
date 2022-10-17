package fetch

import (
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// subsystem shared by all metrics exposed by this package.
	subsystem = "fetch"
)

var (
	totalHits = metrics.NewCounter(
		"cache_hits",
		subsystem,
		"Total hash-to-peer cache hits",
		[]string{"hint"})

	total = metrics.NewCounter(
		"cache_lookup",
		subsystem,
		"Total hash-to-peer cache lookups",
		[]string{"hint"})
)

// logCacheHit logs cache hit.
func logCacheHit(hint datastore.Hint) {
	totalHits.WithLabelValues(string(hint)).Inc()
	total.WithLabelValues(string(hint)).Inc()
}

// logCacheMiss logs cache miss.
func logCacheMiss(hint datastore.Hint) {
	total.WithLabelValues(string(hint)).Inc()
}
