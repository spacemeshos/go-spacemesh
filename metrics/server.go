// Package metrics defines telemetry primitives to be used across components. it uses the prometheus format.
package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spacemeshos/go-spacemesh/log"
)

// StartMetricsServer begins listening and supplying metrics on localhost:`metricsPort`/metrics.
func StartMetricsServer(metricsPort int) {
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%v", metricsPort), nil)
		log.With().Warning("Metrics server stopped: %v", log.Err(err))
	}()
}
