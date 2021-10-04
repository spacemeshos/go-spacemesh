package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem     = "blocks"
	diffTypeLabel = "diff_type"
)

// LayerBlockSize checks average block size.
var LayerBlockSize = metrics.NewHistogramWithBuckets(
	"layer_block_size",
	subsystem,
	"Block size",
	[]string{},
	prometheus.ExponentialBuckets(100, 2, 8),
)

// NumTxsInBlock checks average transaction count in block.
var NumTxsInBlock = metrics.NewHistogramWithBuckets(
	"num_txs_in_block",
	subsystem,
	"Number of transactions in block",
	[]string{},
	prometheus.ExponentialBuckets(1, 2, 8),
)

var baseBlockExceptionLength = metrics.NewHistogramWithBuckets(
	"base_block_exception_length",
	subsystem,
	"Base block exception length",
	[]string{
		diffTypeLabel,
	},
	prometheus.ExponentialBuckets(1, 2, 8),
)

// Blocks diff lengths.
var (
	ForDiffLength     = baseBlockExceptionLength.With(diffTypeLabel, "diff_for")
	AgainstDiffLength = baseBlockExceptionLength.With(diffTypeLabel, "diff_against")
	NeutralDiffLength = baseBlockExceptionLength.With(diffTypeLabel, "diff_neutral")
)
