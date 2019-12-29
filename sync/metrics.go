package sync

import (
	"github.com/go-kit/kit/metrics"
	prmkit "github.com/go-kit/kit/metrics/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

const (
	Namespace = "spacemesh"
	Subsystem = "consensus"
)

func newCounter(name, help string, labels []string) metrics.Counter {
	return prmkit.NewCounterFrom(prometheus.CounterOpts{Namespace: Namespace, Subsystem: Subsystem, Name: name, Help: help}, labels)
}

func newMilliTimer(sum prometheus.Summary) *prometheus.Timer {
	return prometheus.NewTimer(prometheus.ObserverFunc(func(v float64) {
		us := v * 1000 // make microseconds
		sum.Observe(us)
	}))
}

func newFetchRequestTimer(msgtype server.MessageType) *prometheus.Timer {
	if msgtype == BLOCK {
		return newMilliTimer(blockTime)
	}
	if msgtype == TX {
		return newMilliTimer(txTime)
	}
	if msgtype == ATX {
		return newMilliTimer(atxTime)
	}

	return nil
}

var (
	gossipBlockTime = prometheus.NewSummary(prometheus.SummaryOpts{Name: "gossip_block_request_durations",
		Help:       "gossip block handle duration in milliseconds",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}})

	syncLayerTime = prometheus.NewSummary(prometheus.SummaryOpts{Name: "sync_Layer_durations",
		Help:       "Layer Fetch duration in milliseconds",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}})

	blockTime = prometheus.NewSummary(prometheus.SummaryOpts{Name: "block_request_durations",
		Help:       "block requests duration in milliseconds",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}})

	txTime = prometheus.NewSummary(prometheus.SummaryOpts{Name: "tx_request_durations",
		Help:       "tx requests duration in milliseconds",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}})

	atxTime = prometheus.NewSummary(prometheus.SummaryOpts{Name: "atx_request_durations",
		Help:       "atx requests duration in milliseconds",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001}})
)
