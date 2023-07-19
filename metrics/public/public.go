package public

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var Registry = prometheus.NewRegistry()

var (
	Connections = promauto.With(Registry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "smh",
		Name:      "connections",
	}, []string{"dir"})
	initSize = promauto.With(Registry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "smh",
		Name:      "init_size",
	}, []string{"step"})
	InitStart   = initSize.WithLabelValues("start")
	InitEnd     = initSize.WithLabelValues("complete")
	PostSeconds = promauto.With(Registry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "smh",
		Name:      "post_seconds",
	}, []string{}).WithLabelValues()
	Version = promauto.With(Registry).NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "smh",
		Name:      "version",
	}, []string{"version"})
)
