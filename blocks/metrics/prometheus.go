package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem = "blocks"

	// DiffTypeLabel is the label name for the type of different opinions with base block.
	DiffTypeLabel = "diff_type"

	// DiffTypeFor is the label value for the different opinions with base block when voting for blocks.
	DiffTypeFor = "diff_for"
	// DiffTypeAgainst is the label value for the different opinions with base block when voting against blocks.
	DiffTypeAgainst = "diff_against"
	// DiffTypeNeutral is the label value for the different opinions with base block when voting neutral on blocks.
	DiffTypeNeutral = "diff_neutral"
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

// BaseBlockExceptionLength is the size of different opinions between a block and its base block.
var BaseBlockExceptionLength = metrics.NewHistogramWithBuckets(
	"base_block_exception_length",
	subsystem,
	"Base block exception length",
	[]string{
		DiffTypeLabel,
	},
	prometheus.ExponentialBuckets(1, 2, 8),
)
