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
	// the number of valid commit messages
	CommitCounter = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: Namespace,
		Subsystem: Subsystem,
		Name:      "commit_counter",
		Help:      "Number of valid commit messages.",
	}, []string{"set_id"})
)
