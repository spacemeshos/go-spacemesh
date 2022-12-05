package txs

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "txs"

	// labels for tx acceptance state by the cache.
	duplicate           = "dupe"
	saved               = "saved"
	savedNoHdr          = "savedNoHdr"
	cantParse           = "parse"
	cantVerify          = "verify"
	rejectedBadNonce    = "badNonce"
	rejectedInternalErr = "err"
	rawFromDB           = "raw"
	updated             = "updated"

	// label for tx acceptance state by the mempool.
	mempool         = "mempool"
	nonceTooBig     = "nonce"
	balanceTooSmall = "balance"
	tooManyNonce    = "too_many"
	accepted        = "ok"
)

var (
	gossipTxCount = metrics.NewCounter(
		"gossip_txs",
		namespace,
		"number of gossip transactions",
		[]string{"outcome"},
	)
	proposalTxCount = metrics.NewCounter(
		"proposal_txs",
		namespace,
		"number of proposal transactions",
		[]string{"outcome"},
	)
	blockTxCount = metrics.NewCounter(
		"block_txs",
		namespace,
		"number of block transactions",
		[]string{"outcome"},
	)
	rawTxCount = metrics.NewCounter(
		"raw_txs",
		namespace,
		"number of unparsed/raw transactions",
		[]string{"outcome"},
	)
	mempoolTxCount = metrics.NewCounter(
		"mempool_txs",
		namespace,
		"number of transactions added to the mempool",
		[]string{"outcome"},
	)
)

var (
	cacheApplyDuration = metrics.NewHistogramWithBuckets(
		"cache_apply_duration",
		namespace,
		"Duration in ns to apply an layer in conservative cache",
		[]string{},
		prometheus.ExponentialBuckets(100_000_000, 2, 10),
	).WithLabelValues()
	acctResetDuration = metrics.NewHistogramWithBuckets(
		"acct_reset_duration",
		namespace,
		"Duration in ns to apply an layer for an account",
		[]string{},
		prometheus.ExponentialBuckets(10_000_000, 2, 10),
	).WithLabelValues()
)
