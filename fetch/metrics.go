package fetch

import (
	"github.com/prometheus/client_golang/prometheus"
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

	bucketMeshHash = metrics.NewHistogramWithBuckets(
		"bucket_mesh_hash_counts",
		subsystem,
		"requests to mesh hash by bucket",
		[]string{"bucket"},
		prometheus.LinearBuckets(0, 100, 50),
	)

	bucketMeshHashHit = metrics.NewHistogramWithBuckets(
		"bucket_mesh_hits_counts",
		subsystem,
		"requests to mesh hash by bucket hits",
		[]string{},
		[]float64{10, 100, 500, 1000, 2000, 5000, 10000},
	).WithLabelValues()
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
