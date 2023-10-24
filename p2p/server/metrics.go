package server

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace  = "server"
	protoLabel = "protocol"
)

var (
	targetQueue = metrics.NewGauge(
		"target_queue",
		namespace,
		"target size of the queue",
		[]string{protoLabel},
	)
	queue = metrics.NewGauge(
		"queue",
		namespace,
		"actual size of the queue",
		[]string{protoLabel},
	)
	targetRps = metrics.NewGauge(
		"rps",
		namespace,
		"target requests per second",
		[]string{protoLabel},
	)
	requests = metrics.NewCounter(
		"requests",
		namespace,
		"requests counter",
		[]string{protoLabel, "state"},
	)
	clientLatency = metrics.NewHistogramWithBuckets(
		"client_latency_seconds",
		namespace,
		"latency since initiating a request",
		[]string{protoLabel, "result"},
		prometheus.ExponentialBuckets(0.01, 2, 10),
	)
	serverLatency = metrics.NewHistogramWithBuckets(
		"server_latency_seconds",
		namespace,
		"latency since accepting new stream",
		[]string{protoLabel},
		prometheus.ExponentialBuckets(0.01, 2, 10),
	)
)

func newTracker(protocol string) *tracker {
	return &tracker{
		targetQueue:          targetQueue.WithLabelValues(protocol),
		queue:                queue.WithLabelValues(protocol),
		targetRps:            targetRps.WithLabelValues(protocol),
		completed:            requests.WithLabelValues(protocol, "completed"),
		accepted:             requests.WithLabelValues(protocol, "accepted"),
		dropped:              requests.WithLabelValues(protocol, "dropped"),
		serverLatency:        serverLatency.WithLabelValues(protocol),
		clientLatency:        clientLatency.WithLabelValues(protocol, "success"),
		clientLatencyFailure: clientLatency.WithLabelValues(protocol, "failure"),
	}
}

type tracker struct {
	targetQueue                         prometheus.Gauge
	queue                               prometheus.Gauge
	targetRps                           prometheus.Gauge
	completed                           prometheus.Counter
	accepted                            prometheus.Counter
	dropped                             prometheus.Counter
	serverLatency                       prometheus.Observer
	clientLatency, clientLatencyFailure prometheus.Observer
}
