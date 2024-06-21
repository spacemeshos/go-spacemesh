package hare3

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const namespace = "hare"

var (
	processCounter = metrics.NewCounter(
		"session",
		namespace,
		"number of hare sessions at different stages",
		[]string{"stage"},
	)
	sessionStart      = processCounter.WithLabelValues("started")
	sessionTerminated = processCounter.WithLabelValues("terminated")
	sessionCoin       = processCounter.WithLabelValues("weakcoin")
	sessionResult     = processCounter.WithLabelValues("result")

	exitErrors = metrics.NewCounter(
		"exit_errors",
		namespace,
		"number of unexpected exit errors. should remain at zero",
		[]string{},
	).WithLabelValues()
	validationError = metrics.NewCounter(
		"validation_errors",
		namespace,
		"number of validation errors. not expected to be at zero",
		[]string{"error"},
	)
	notRegisteredError = validationError.WithLabelValues("not_registered")
	malformedError     = validationError.WithLabelValues("malformed")
	signatureError     = validationError.WithLabelValues("signature")
	oracleError        = validationError.WithLabelValues("oracle")

	droppedMessages = metrics.NewCounter(
		"dropped_msgs",
		namespace,
		"number of messages dropped by gossip",
		[]string{},
	).WithLabelValues()

	validationLatency = metrics.NewHistogramWithBuckets(
		"validation_seconds",
		namespace,
		"validation time in seconds",
		[]string{"step"},
		prometheus.ExponentialBuckets(0.01, 2, 10),
	)
	oracleLatency = validationLatency.WithLabelValues("oracle")
	submitLatency = validationLatency.WithLabelValues("submit")

	protocolLatency = metrics.NewHistogramWithBuckets(
		"protocol_seconds",
		namespace,
		"protocol time in seconds",
		[]string{"step"},
		prometheus.ExponentialBuckets(0.01, 2, 10),
	)
	proposalsLatency = protocolLatency.WithLabelValues("proposals")
	activeLatency    = protocolLatency.WithLabelValues("active")
)
