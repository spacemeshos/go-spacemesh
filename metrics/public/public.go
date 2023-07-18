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
)
