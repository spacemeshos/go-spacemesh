package miner

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

var proposalBuild = metrics.NewHistogramWithBuckets(
	"proposal_build_seconds",
	"miner",
	"duration to build a proposal in seconds",
	[]string{},
	prometheus.ExponentialBuckets(0.1, 2, 10),
).WithLabelValues()

type latencyTracker struct {
	start    time.Time
	data     time.Time
	tortoise time.Time
	txs      time.Time
	hash     time.Time
	publish  time.Time
}

func (lt *latencyTracker) total() time.Duration {
	return lt.publish.Sub(lt.start)
}

func (lt *latencyTracker) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddDuration("data", lt.data.Sub(lt.start))
	encoder.AddDuration("tortoise", lt.tortoise.Sub(lt.data))
	encoder.AddDuration("hash", lt.hash.Sub(lt.tortoise))
	encoder.AddDuration("txs", lt.txs.Sub(lt.hash))
	encoder.AddDuration("publish", lt.publish.Sub(lt.hash))
	total := lt.total()
	encoder.AddDuration("total", total)
	// arbitrary threshold that we want to highlight as a problem
	if total > 10*time.Second {
		encoder.AddBool("LATE PROPOSAL", true)
	}
	return nil
}
