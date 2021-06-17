package metrics

import (
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/spacemeshos/go-spacemesh/log"
	"time"
)

// StartPushingMetrics begins pushing metrics to the url specified by the --metrics-push flag
// with period specified by the --metrics-push-period flag
func StartPushingMetrics(url string, periodSec int, nodeID, networkID string) {
	period := time.Duration(periodSec) * time.Second
	ticker := time.Tick(period)

	pusher := push.New(url, "go-spacemesh").Gatherer(stdprometheus.DefaultGatherer).
		Grouping("node_id", nodeID).
		Grouping("network_id", networkID)

	go func() {
		for range ticker {
			err := pusher.Push()
			if err != nil {
				log.With().Warning("failed to push metrics", log.Err(err))
			}
		}
	}()
}
