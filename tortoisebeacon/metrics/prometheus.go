package metrics

import "github.com/spacemeshos/go-spacemesh/metrics"

const (
	subsystem = "beacons"
	// LabelEpoch is the metric label for the epoch value.
	LabelEpoch = "epoch"
	// LabelBeacon is the metric label for the beacon value.
	LabelBeacon = "beacon"
)

var (
	// ObservedBeaconCounter is the total count of each beacon reported by blocks.
	ObservedBeaconCounter = metrics.NewCounter(
		"observed_beacon_count",
		subsystem,
		"Weight of each beacon collected from blocks for each epoch and value",
		[]string{"epoch", "beacon"},
	)

	// ObservedBeaconWeightCounter is the total weight of each beacon reported by blocks.
	ObservedBeaconWeightCounter = metrics.NewCounter(
		"observed_beacon_weight",
		subsystem,
		"Weight of each beacon collected from blocks for each epoch and value",
		[]string{"epoch", "beacon"},
	)

	// CalculatedBeaconCounter is the weight of the beacon calculated by the node.
	CalculatedBeaconCounter = metrics.NewCounter(
		"calculated_beacon_weight",
		subsystem,
		"Weight of each beacon calculated by the node for each epoch",
		[]string{"epoch", "beacon"},
	)
)
