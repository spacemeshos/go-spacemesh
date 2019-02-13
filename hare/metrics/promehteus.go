package metrics

import (
	"github.com/go-kit/kit/metrics/prometheus"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
)

const (
	// Namespace is the metrics namespace //todo: figure out if this can be used better.
	Namespace = "spacemesh"
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	Subsystem = "hare"
)

var (
	// the number of valid commits for a set
	CommitCounter = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "commit_counter",
		Help:      "Number of valid commit messages for a set",
	}, []string{"set_id"})

	// the number of valid notifications for a set
	NotifyCounter = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "notify_counter",
		Help:      "Number of valid notifications for a set",
	}, []string{"set_id"})

	// the number of valid messages for each type
	MessageTypeCounter = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "message_type_counter",
		Help:      "Number of valid messages sent to processing for each type",
	}, []string{"type_id"})

	// the number of pre-round msgs for each value
	PreRoundCounter = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "pre_round_counter",
		Help:      "Number of pre-round msgs for each value",
	}, []string{"value"})

	// the total number of current consensus processes
	TotalConsensusProcesses = prometheus.NewGaugeFrom(stdprometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "total_consensus_processes",
		Help:      "The total number of current consensus processes running",
	}, []string{})
)
