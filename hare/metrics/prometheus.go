package metrics

import (
	"github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

const (
	// Namespace is the metrics namespace //todo: figure out if this can be used better.
	Namespace = "spacemesh"
	// Subsystem is a subsystem shared by all metrics exposed by this package.
	Subsystem = "hare"
)

var (
	// MessageTypeCounter is the number of valid messages per type.
	MessageTypeCounter = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "message_type_counter",
		Help:      "Number of valid messages sent to processing for each type",
	}, []string{"type_id", "layer", "reporter"})

	// TotalConsensusProcesses is the total number of current consensus processes.
	TotalConsensusProcesses = prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "total_consensus_processes",
		Help:      "The total number of current consensus processes running",
	}, []string{"layer"})
)
