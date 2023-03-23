package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Namespace is the basic namespace where all metrics are defined under.
	Namespace = "spacemesh"
)

// NewCounter creates a Counter metrics under the global namespace returns nop if metrics are disabled.
func NewCounter(name, subsystem, help string, labels []string) *prometheus.CounterVec {
	return promauto.NewCounterVec(prometheus.CounterOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

// NewGauge creates a Gauge metrics under the global namespace returns nop if metrics are disabled.
func NewGauge(name, subsystem, help string, labels []string) *prometheus.GaugeVec {
	return promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

// NewHistogram creates a Histogram metrics under the global namespace returns nop if metrics are disabled.
func NewHistogram(name, subsystem, help string, labels []string) *prometheus.HistogramVec {
	return promauto.NewHistogramVec(prometheus.HistogramOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

// NewHistogramWithBuckets creates a Histogram metrics with custom buckets.
func NewHistogramWithBuckets(name, subsystem, help string, labels []string, buckets []float64) *prometheus.HistogramVec {
	return promauto.NewHistogramVec(prometheus.HistogramOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help, Buckets: buckets}, labels)
}

// receivedMessagesLatency measures the time a message was received relative to
// the time it should be sent in the protocol. Metrics are labeled by protocol
// and sign. Sign is either "pos" or "neg" and is chosen depending on the sign
// of the observed latency. Negative latencies occur when a message is received
// before the time it should be sent, this could happen in a network where
// nodes' clocks are not in sync.
var receivedMessagesLatency = NewHistogramWithBuckets(
	"message_latency_seconds",
	"",
	"Observed latency for message",
	[]string{"protocol", "sign"},
	prometheus.ExponentialBuckets(0.1, 2, 12),
)

func ReportMessageLatency(protocol string, latency time.Duration) {
	seconds := latency.Seconds()
	sign := "pos"
	if seconds < 0 {
		sign = "neg"
		// If the observation is negative make it positive.
		seconds = -seconds
	}
	receivedMessagesLatency.WithLabelValues(protocol, sign).Observe(seconds)
}
