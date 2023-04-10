package metrics

import (
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
