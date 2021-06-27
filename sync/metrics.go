package sync

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
	// "github.com/spacemeshos/go-spacemesh/p2p/server"
)

const (
	// MetricsSubsystem is a subsystem shared by all metrics exposed by this
	// package.
	MetricsSubsystem = "consensus"
)

/*
func newCounter(name, help string, labels []string) metrics.Counter {
	return prmkit.NewCounterFrom(prometheus.CounterOpts{Namespace: namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

func newMilliTimer(sum prometheus.Summary) *prometheus.Timer {
	return prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		us := v * 1000 // make microseconds
		sum.Observe(us)
	}))
}

func newFetchRequestTimer(msgtype server.MessageType) *prometheus.Timer {
	if msgtype == blockMsg {
		return newMilliTimer(blockTime)
	}
	if msgtype == txMsg {
		return newMilliTimer(txTime)
	}
	if msgtype == atxMsg {
		return newMilliTimer(atxTime)
	}

	return nil
}
*/

var (
	gossipBlockTime = metrics.NewSummary(
		"gossip_block_request_durations",
		MetricsSubsystem,
		"Gossip block handle duration in milliseconds",
		nil,
		map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	)

	syncLayerTime = metrics.NewSummary(
		"sync_layer_durations",
		MetricsSubsystem,
		"Layer Fetch duration in milliseconds",
		nil,
		map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	)

	blockTime = metrics.NewSummary(
		"block_request_durations",
		MetricsSubsystem,
		"Block requests duration in milliseconds",
		nil,
		map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	)

	txTime = metrics.NewSummary(
		"tx_request_durations",
		MetricsSubsystem,
		"Tx requests duration in milliseconds",
		nil,
		map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	)

	atxTime = metrics.NewSummary(
		"atx_request_durations",
		MetricsSubsystem,
		"atx requests duration in milliseconds",
		nil,
		map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	)
)
