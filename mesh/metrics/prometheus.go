package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "mesh"
)

// LayerIDLabel is metric label for layer ID.
const (
	LayerIDLabel = "layer_id"
)

// LayerNumBlocks is number of blocks in layer.
var LayerNumBlocks = metrics.NewIntegerHistogram(
	"layer_num_blocks",
	Subsystem,
	"Number of blocks in layer",
	[]string{
		LayerIDLabel,
	},
)
