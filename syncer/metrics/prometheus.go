package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "syncer"
)

// LayerNumBlocks checks how network disagrees on layer content.
var LayerNumBlocks = metrics.NewHistogram(
	"layer_num_blocks",
	Subsystem,
	"How network disagrees on layer content",
	[]string{
		"layer_id",
	},
)
