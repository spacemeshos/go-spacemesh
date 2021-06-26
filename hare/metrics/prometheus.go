package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this package.
	MetricsSubsystem = "hare"
)

var (
	// MessageTypeCounter is the number of valid messages per type.
	MessageTypeCounter = metrics.NewCounter(
		"message_type_counter",
		MetricsSubsystem,
		"Number of valid messages sent to processing for each type",
		[]string{"type_id", "layer", "reporter"},
	)

	// TotalConsensusProcesses is the total number of current consensus processes.
	TotalConsensusProcesses = metrics.NewGauge(
		"total_consensus_processes",
		MetricsSubsystem,
		"The total number of current consensus processes running",
		[]string{"layer"},
	)
)
