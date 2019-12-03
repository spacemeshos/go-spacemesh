package metrics

import (
	"context"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spacemeshos/go-spacemesh/log"
	"net/http"
)

func StartCollectingMetrics(ctx context.Context, metricsPort int) {
	Enabled = true
	log.Info("Start metrics server")
	http.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: fmt.Sprintf(":%v", metricsPort)}

	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.TODO()); err != nil {
			log.Error("Error during shutdown err=%v", err)
		}
	}()

	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			log.Error("cannot start http server", err)
		}
	}()
}
