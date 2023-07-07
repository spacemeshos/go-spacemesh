package peersync

import "github.com/spacemeshos/go-spacemesh/metrics"

var offsetGauge = metrics.NewGauge(
	"peers_offset",
	"clock",
	"local clock difference with peers local clock in seconds",
	[]string{},
).WithLabelValues()
