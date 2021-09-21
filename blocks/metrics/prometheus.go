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

// AvgLayerBlockSize checks average block size.
var AvgLayerBlockSize = metrics.NewHistogram(
	"avg_layer_block_size",
	subsystem,
	"Average block size",
	[]string{
		LayerIDLabel,
		BlockIDLabel,
	},
)

// AvgNumTxsInBlock checks average transaction count in block.
var AvgNumTxsInBlock = metrics.NewHistogram(
	"avg_num_txs_in_block",
	subsystem,
	"Average number of transactions in block",
	[]string{
		LayerIDLabel,
		BlockIDLabel,
	},
)

var avgBaseBlockExceptionLength = metrics.NewHistogram(
	"avg_base_block_exception_length",
	subsystem,
	"Average base block exception length",
	[]string{
		LayerIDLabel,
		BlockIDLabel,
		diffTypeLabel,
	},
)

// Average blocks diff lengths.
var (
	AvgForDiffLength     = avgBaseBlockExceptionLength.With(diffTypeLabel, "for_diff")
	AvgAgainstDiffLength = avgBaseBlockExceptionLength.With(diffTypeLabel, "against_diff")
	AvgNeutralDiffLength = avgBaseBlockExceptionLength.With(diffTypeLabel, "neutral_diff")
)
