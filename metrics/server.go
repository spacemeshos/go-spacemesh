// Package metrics define telemetry primitives to use across components. it uses the prometheus format.
package metrics

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spacemeshos/go-spacemesh/log"
	"net/http"
)

// StartCollectingMetrics begins listening and supplying metrics on localhost:`metricsPort`/metrics
func StartCollectingMetrics(metricsPort int) {
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%v", metricsPort), nil)
		log.With().Warning("Metrics server stopped: %v", log.Err(err))
	}()
}
