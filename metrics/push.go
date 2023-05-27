package metrics

import (
	"time"

	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"

	"github.com/spacemeshos/go-spacemesh/log"
)

// StartPushingMetrics begins pushing metrics to the url specified by the --metrics-push flag
// with period specified by the --metrics-push-period flag.
func StartPushingMetrics(url string, username string, password string, periodSec int, nodeID, networkID string) {
	period := time.Duration(periodSec) * time.Second
	ticker := time.NewTicker(period)

	pusher := push.New(url, "go-spacemesh").Gatherer(stdprometheus.DefaultGatherer).
		Grouping("node_id", nodeID).
		Grouping("network_id", networkID)

	if username != "" && password != "" {
		pusher.BasicAuth(username, password)
	}

	go func() {
		for range ticker.C {
			err := pusher.Push()
			if err != nil {
				log.With().Warning("failed to push metrics", log.Err(err))
			}
		}
	}()
}
