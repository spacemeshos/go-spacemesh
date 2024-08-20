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
	start time.Time
	end   time.Time

	data     time.Duration
	tortoise time.Duration
	hash     time.Duration
	txs      time.Duration
	publish  time.Duration
}

func (lt *latencyTracker) total() time.Duration {
	return lt.end.Sub(lt.start)
}

func (lt *latencyTracker) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddDuration("data", lt.data)
	encoder.AddDuration("tortoise", lt.tortoise)
	encoder.AddDuration("hash", lt.hash)
	encoder.AddDuration("txs", lt.txs)
	encoder.AddDuration("publish", lt.publish)
	total := lt.total()
	encoder.AddDuration("total", total)
	// arbitrary threshold that we want to highlight as a problem
	if total > 10*time.Second {
		encoder.AddBool("LATE PROPOSAL", true)
	}
	return nil
}
