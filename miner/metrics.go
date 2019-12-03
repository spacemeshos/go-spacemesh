package miner

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// Namespace is the metrics namespace //todo: figure out if this can be used better.
	Namespace = "spacemesh"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "miner"

	LayerId = "layer_id"
)

// todo: maybe add functions that attach peer_id and protocol. (or other labels) without writing label names.

// TotalPeers is the total number of peers we have connected
var (
	RecievedAtxs = metrics.NewGauge("atxs_per_layer", "miner", "shows atxs per layer", []string{LayerId})
)
