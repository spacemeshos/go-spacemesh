package consensus

import (
	"github.com/go-kit/kit/metrics"
	prmkit "github.com/go-kit/kit/metrics/prometheus"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	Namespace = "spacemesh"
	Subsystem = "consensus"
)

func getGauge(name, help string, lables []string) metrics.Gauge {
	return prmkit.NewGaugeFrom(prometheus.GaugeOpts{Namespace: Namespace, Subsystem: Subsystem, Name: name, Help: help}, lables)
}

var (
	pbaseCount     = getGauge("pbase_counter", "pbase index", []string{"pbase_index"})
	processedCount = getGauge("processed_index", "processed index", []string{"processed_index"})
	blockVotes     = getGauge("block_votes", "block validity", []string{"validity"})
	validBlocks    = blockVotes.With("Validity", "valid")
	invalidBlocks  = blockVotes.With("Validity", "invalid")
)
