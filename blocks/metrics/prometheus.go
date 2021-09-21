package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem     = "blocks"
	diffTypeLabel = "diff_type"
)

// Metrics labels.
const (
	LayerIDLabel = "layer_id"
	BlockIDLabel = "block_id"
)

// LayerBlockSize checks average block size.
var LayerBlockSize = metrics.NewHistogram(
	"layer_block_size",
	subsystem,
	"Block size",
	[]string{
		LayerIDLabel,
		BlockIDLabel,
	},
)

// NumTxsInBlock checks average transaction count in block.
var NumTxsInBlock = metrics.NewHistogram(
	"num_txs_in_block",
	subsystem,
	"Number of transactions in block",
	[]string{
		LayerIDLabel,
		BlockIDLabel,
	},
)

var baseBlockExceptionLength = metrics.NewHistogram(
	"base_block_exception_length",
	subsystem,
	"Base block exception length",
	[]string{
		LayerIDLabel,
		BlockIDLabel,
		diffTypeLabel,
	},
)

// Blocks diff lengths.
var (
	ForDiffLength     = baseBlockExceptionLength.With(diffTypeLabel, "for_diff")
	AgainstDiffLength = baseBlockExceptionLength.With(diffTypeLabel, "against_diff")
	NeutralDiffLength = baseBlockExceptionLength.With(diffTypeLabel, "neutral_diff")
)
