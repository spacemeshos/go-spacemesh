package tortoise

import (
	"github.com/go-kit/kit/metrics"
	prmkit "github.com/go-kit/kit/metrics/prometheus"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	Namespace = "spacemesh"
	Subsystem = "consensus"
)

func newGauge(name, help string, labels []string) metrics.Gauge {
	return prmkit.NewGaugeFrom(prometheus.GaugeOpts{Namespace: Namespace, Subsystem: Subsystem, Name: name, Help: help}, labels)
}

var (
	pbaseCount     = newGauge("pbase_counter", "pbase index", []string{})
	processedCount = newGauge("processed_index", "number of layers processed", []string{})
	blockVotes     = newGauge("block_votes", "block validity", []string{"validity"})
	validBlocks    = blockVotes.With("validity", "valid")
	invalidBlocks  = blockVotes.With("validity", "invalid")
)
