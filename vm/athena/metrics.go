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
		prometheus.ExponentialBuckets(10_000, 2, 10),
	).WithLabelValues()
	transactionDurationParse = metrics.NewHistogramWithBuckets(
		"transaction_duration_parse",
		namespace,
		"Duration in ns for parsing a transaction",
		[]string{},
		prometheus.ExponentialBuckets(10_000, 2, 10),
	).WithLabelValues()
	transactionDurationVerify = metrics.NewHistogramWithBuckets(
		"transaction_duration_verify",
		namespace,
		"Duration in ns for verifying a transaction",
		[]string{},
		prometheus.ExponentialBuckets(10_000, 2, 10),
	).WithLabelValues()
	transactionDurationExecute = metrics.NewHistogramWithBuckets(
		"transaction_duration_execute",
		namespace,
		"Duration in ns for executing a transaction",
		[]string{},
		prometheus.ExponentialBuckets(10_000, 2, 10),
	).WithLabelValues()
)

var (
	blockDuration = metrics.NewHistogramWithBuckets(
		"block_duration",
		namespace,
		"Duration in ns with update to disk",
		[]string{},
		prometheus.ExponentialBuckets(10_000_000, 2, 10),
	).WithLabelValues()
	blockDurationWait = metrics.NewHistogramWithBuckets(
		"block_duration_wait",
		namespace,
		"Duration in ns for db to be ready to process transactions.",
		[]string{},
		prometheus.ExponentialBuckets(100_000, 2, 10),
	).WithLabelValues()
	blockDurationTxs = metrics.NewHistogramWithBuckets(
		"block_duration_txs",
		namespace,
		"Duration in ns to parse and execute transactions.",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 2, 10),
	).WithLabelValues()
	blockDurationRewards = metrics.NewHistogramWithBuckets(
		"block_duration_rewards",
		namespace,
		"Duration in ns to compute and update rewards.",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 2, 10),
	).WithLabelValues()
	blockDurationPersist = metrics.NewHistogramWithBuckets(
		"block_duration_persist",
		namespace,
		"Duration in ns to write updates on disk.",
		[]string{},
		prometheus.ExponentialBuckets(10_000_000, 2, 10),
	).WithLabelValues()
)

var writesPerBlock = metrics.NewHistogramWithBuckets(
	"account_writes_per_block",
	namespace,
	"Number of touched (updated) accounts in the block",
	[]string{},
	prometheus.ExponentialBuckets(100, 10, 10),
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
		prometheus.ExponentialBuckets(100, 10, 10),
	).WithLabelValues()
)

var (
	feesCount = metrics.NewCounter(
		"fees",
		namespace,
		"Transaction fees",
		[]string{},
	).WithLabelValues()
	subsidyCount = metrics.NewCounter(
		"subsidy",
		namespace,
		"Estimated subsidy",
		[]string{},
	).WithLabelValues()
	rewardsCount = metrics.NewCounter(
		"rewards",
		namespace,
		"Total rewards including issuence and fees",
		[]string{},
	).WithLabelValues()
	burntCount = metrics.NewCounter(
		"rewards_burn",
		namespace,
		"Burnt amount of issuence and fees",
		[]string{},
	).WithLabelValues()
)

var appliedLayer = metrics.NewGauge(
	"applied_layer",
	namespace,
	"Applied layer",
	[]string{},
).WithLabelValues()
