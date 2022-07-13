package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const system = "vm"

var (
	TransactionDuration = metrics.NewHistogramWithBuckets(
		"transaction_duration",
		system,
		"Duration in ns without writing updated state on disk",
		[]string{"step"},
		prometheus.LinearBuckets(100_000, 100_000, 10),
	)
	// TransactionDurationParse ns to parse transaction header and args.
	TransactionDurationParse = TransactionDuration.WithLabelValues("parse")
	// TransactionDurationVerify ns verify transaction.
	TransactionDurationVerify = TransactionDuration.WithLabelValues("verify")
	// TransactionDurationExecute ns to execute transaction.
	TransactionDurationExecute = TransactionDuration.WithLabelValues("execute")
)

var (
	BlockDuration = metrics.NewHistogramWithBuckets(
		"block_duration",
		system,
		"Duration in ns with update to disk",
		[]string{"step"},
		prometheus.LinearBuckets(10_000_000, 100_000_000, 10),
	)
	BlockDurationWait    = BlockDuration.WithLabelValues("wait")
	BlockDurationTxs     = BlockDuration.WithLabelValues("txs")
	BlockDurationRewards = BlockDuration.WithLabelValues("rewards")
	BlockDurationPersist = BlockDuration.WithLabelValues("persist")
)

var StagingCacheSize = metrics.NewGauge(
	"staging_cache_size",
	system,
	"Number of loaded accounts into the cache",
	[]string{},
)

var (
	Tx = metrics.NewCounter(
		"txs",
		system,
		"Number of transactions",
		[]string{},
	)

	invalidTx = metrics.NewCounter(
		"invalid_txs",
		system,
		"Number of invalid transactions.",
		[]string{"type"},
	)
	InvalidTxNonce  = invalidTx.WithLabelValues("nonce")
	InvalidTxSpawn  = invalidTx.WithLabelValues("spawn")
	InvalidTxMaxGas = invalidTx.WithLabelValues("max_gas")

	TransactionsPerBlock = metrics.NewHistogramWithBuckets(
		"transactions_per_block",
		system,
		"Number of transactions in the block",
		[]string{},
		prometheus.LinearBuckets(100, 1000, 10),
	)
)

var (
	TouchedAccountsPerTx = metrics.NewHistogramWithBuckets(
		"touched_accounts_per_tx",
		system,
		"Number of touched (updated) accounts in transaction",
		[]string{},
		prometheus.LinearBuckets(1, 1, 10),
	)
	TouchedAccountsPerBlock = metrics.NewHistogramWithBuckets(
		"touched_accounts_per_block",
		system,
		"Number of touched (updated) accounts in the block",
		[]string{},
		prometheus.LinearBuckets(10, 100, 10),
	)
)

var (
	Fees = metrics.NewCounter(
		"fees",
		system,
		"Fees in coins",
		[]string{},
	)
	Rewards = metrics.NewCounter(
		"rewards",
		system,
		"Total rewards including issuence and fees",
		[]string{},
	)
)
