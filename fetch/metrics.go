package fetch

import (
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// subsystem shared by all metrics exposed by this package.
	subsystem = "fetch"
	hint      = "hint"
)

var (
	totalHits = metrics.NewCounter(
		"cache_hits",
		subsystem,
		"Total hash-to-peer cache hits",
		[]string{hint})

	total = metrics.NewCounter(
		"cache_lookup",
		subsystem,
		"Total hash-to-peer cache lookups",
		[]string{hint})

	totalHashReqs = metrics.NewCounter(
		"hash_reqs",
		subsystem,
		"total hash requests",
		[]string{hint})

	hashMissing = metrics.NewCounter(
		"hash_missing",
		subsystem,
		"total requests that hash is not present locally",
		[]string{hint})

	hashEmptyData = metrics.NewCounter(
		"hash_empty",
		subsystem,
		"total request that hash has no data",
		[]string{hint})

	peerErrors = metrics.NewCounter(
		"hash_peer_err",
		subsystem,
		"total error from sending peers hash requests",
		[]string{hint})

	certReq = metrics.NewCounter(
		"certs",
		subsystem,
		"total requests for block certificate received",
		[]string{}).WithLabelValues()

	opnReqV2 = metrics.NewCounter(
		"opn_reqs",
		subsystem,
		"total layer opinion requests received",
		[]string{"version"},
	).WithLabelValues("v2")
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
