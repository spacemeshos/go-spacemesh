package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "syncer"
)

var LayerNumBlocks = metrics.NewHistogram(
	"layer_num_blocks",
	Subsystem,
	"How network disagrees on layer content",
	[]string{
		"layer_id",
	},
)
