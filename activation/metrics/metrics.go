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
