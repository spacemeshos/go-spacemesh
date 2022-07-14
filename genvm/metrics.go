package vm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const namespace = "vm"

var (
	transactionDuration = metrics.NewHistogramWithBuckets(
		"transaction_duration",
		namespace,
		"Duration in ns without writing updated state on disk",
		[]string{},
		prometheus.LinearBuckets(100_000, 100_000, 10),
	).WithLabelValues()
	transactionDurationParse = metrics.NewHistogramWithBuckets(
		"transaction_duration_parse",
		namespace,
		"Duration in ns for parsing a transaction",
		[]string{},
		prometheus.LinearBuckets(100_000, 100_000, 10),
	).WithLabelValues()
	transactionDurationVerify = metrics.NewHistogramWithBuckets(
		"transaction_duration_verify",
		namespace,
		"Duration in ns for verifying a transaction",
		[]string{},
		prometheus.LinearBuckets(100_000, 100_000, 10),
	).WithLabelValues()
	transactionDurationExecute = metrics.NewHistogramWithBuckets(
		"transaction_duration_execute",
		namespace,
		"Duration in ns for executing a transaction",
		[]string{},
		prometheus.LinearBuckets(100_000, 100_000, 10),
	).WithLabelValues()

	blockDuration = metrics.NewHistogramWithBuckets(
		"block_duration",
		namespace,
		"Duration in ns with update to disk",
		[]string{},
		prometheus.LinearBuckets(10_000_000, 100_000_000, 10),
	).WithLabelValues()
	blockDurationWait = metrics.NewHistogramWithBuckets(
		"block_duration_wait",
		namespace,
		"Duration in ns for db to be ready to process transactions.",
		[]string{},
		prometheus.LinearBuckets(10_000_000, 100_000_000, 10),
	).WithLabelValues()
	blockDurationTxs = metrics.NewHistogramWithBuckets(
		"block_duration_txs",
		namespace,
		"Duration in ns to parse and execute transactions.",
		[]string{},
		prometheus.LinearBuckets(10_000_000, 100_000_000, 10),
	).WithLabelValues()
	blockDurationRewards = metrics.NewHistogramWithBuckets(
		"block_duration_rewards",
		namespace,
		"Duration in ns to compute and update rewards.",
		[]string{},
		prometheus.LinearBuckets(10_000_000, 100_000_000, 10),
	).WithLabelValues()
	blockDurationPersist = metrics.NewHistogramWithBuckets(
		"block_duration_persist",
		namespace,
		"Duration in ns to write updates on disk.",
		[]string{},
		prometheus.LinearBuckets(10_000_000, 100_000_000, 10),
	).WithLabelValues()
)

var stagingCacheSize = metrics.NewGauge(
	"staging_cache_size",
	namespace,
	"Number of loaded accounts into the cache",
	[]string{},
).WithLabelValues()

var (
	txCount = metrics.NewCounter(
		"txs",
		namespace,
		"Number of transactions",
		[]string{},
	).WithLabelValues()

	invalidTxCount = metrics.NewCounter(
		"invalid_txs",
		namespace,
		"Number of invalid transactions.",
		[]string{},
	).WithLabelValues()

	transactionsPerBlock = metrics.NewHistogramWithBuckets(
		"transactions_per_block",
		namespace,
		"Number of transactions in the block",
		[]string{},
		prometheus.LinearBuckets(100, 1000, 10),
	).WithLabelValues()
)

var (
	touchedAccountsPerTx = metrics.NewHistogramWithBuckets(
		"touched_accounts_per_tx",
		namespace,
		"Number of touched (updated) accounts in transaction",
		[]string{},
		prometheus.LinearBuckets(1, 1, 10),
	).WithLabelValues()
	touchedAccountsPerBlock = metrics.NewHistogramWithBuckets(
		"touched_accounts_per_block",
		namespace,
		"Number of touched (updated) accounts in the block",
		[]string{},
		prometheus.LinearBuckets(10, 100, 10),
	).WithLabelValues()
)

var (
	feesCount = metrics.NewCounter(
		"fees",
		namespace,
		"Fees in coins",
		[]string{},
	).WithLabelValues()
	rewardsCount = metrics.NewCounter(
		"rewards",
		namespace,
		"Total rewards including issuence and fees",
		[]string{},
	).WithLabelValues()
)
