package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem = "hare"
)

var (
	// MessageTypeCounter is the number of valid messages per type.
	MessageTypeCounter = metrics.NewCounter(
		"message_type_counter",
		subsystem,
		"Number of valid messages sent to processing for each type",
		[]string{"type_id", "layer", "reporter"},
	)

	// TotalConsensusProcesses is the total number of current consensus processes.
	TotalConsensusProcesses = metrics.NewGauge(
		"total_consensus_processes",
		subsystem,
		"The total number of current consensus processes running",
		[]string{"layer"},
	)
)
