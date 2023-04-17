package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "activation"
)

var PostDuration = metrics.NewGauge(
	"post_duration",
	namespace,
	"duration of last PoST in nanoseconds",
	[]string{},
).WithLabelValues()

var PoetPowDuration = metrics.NewGauge(
	"poet_pow_duration",
	namespace,
	"duration of last PoET Proof of Work in nanoseconds",
	[]string{},
).WithLabelValues()

var (
	publishWindowLatency = metrics.NewHistogramWithBuckets(
		"publish_window_seconds",
		namespace,
		"how much time in seconds you have before window for poet registrations closes",
		[]string{"condition"},
		prometheus.ExponentialBuckets(1, 2, 10),
	)
	PublishOntimeWindowLatency = publishWindowLatency.WithLabelValues("ontime")
	PublishLateWindowLatency   = publishWindowLatency.WithLabelValues("late")
)
