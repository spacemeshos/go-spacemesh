package hare4

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const namespace = "hare4" // todo change this back to `hare`

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

	requestCompactCounter = prometheus.NewCounter(metrics.NewCounterOpts(
		namespace,
		"request_compact_count",
		"number of times we needed to go into a clarifying round",
	))
	requestCompactErrorCounter = prometheus.NewCounter(metrics.NewCounterOpts(
		namespace,
		"request_compact_error_count",
		"number of errors got when requesting compact proposals from peer",
	))
	requestCompactHandlerCounter = prometheus.NewCounter(metrics.NewCounterOpts(
		namespace,
		"request_compact_handler_count",
		"number of requests handled on the compact stream handler",
	))
	messageCacheMiss = prometheus.NewCounter(metrics.NewCounterOpts(
		namespace,
		"message_cache_miss",
		"number of message cache misses",
	))
	messageCompactsCounter = prometheus.NewCounter(metrics.NewCounterOpts(
		namespace,
		"message_compacts_count",
		"number of compact proposals that arrived to be checked in a message",
	))

	messageCompactFetchCounter = prometheus.NewCounter(metrics.NewCounterOpts(
		namespace,
		"message_compact_fetch_count",
		"how many compact prefixes need to be fetched because they cannot be matched locally",
	))
	preroundSigFailCounter = prometheus.NewCounter(metrics.NewCounterOpts(
		namespace,
		"preround_signature_fail_count",
		"counter for signature fails on preround with compact message",
	))
)
