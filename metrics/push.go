package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/push"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
)

// StartPushingMetrics begins pushing metrics to the url specified by the --metrics-push flag
// with period specified by the --metrics-push-period flag.
func StartPushingMetrics(url, username, password string, headers map[string]string, period time.Duration, nodeID, networkID string) {
	header := http.Header{}
	for k, v := range headers {
		header.Add(k, v)
	}
	pusher := push.New(url, "go-spacemesh").Gatherer(public.Registry).
		Grouping("node", nodeID).
		Grouping("network", networkID).
		Header(header)
	if username != "" && password != "" {
		pusher = pusher.BasicAuth(username, password)
	}
	go func() {
		ticker := time.NewTicker(period)
		for range ticker.C {
			err := pusher.Push()
			if err != nil {
				log.With().Warning("failed to push metrics", log.Err(err))
			}
		}
	}()
}
