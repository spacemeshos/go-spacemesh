package blocks

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "blocks"

	// labels for block generation.
	failFetch   = "fail_proposal"
	failGen     = "fail_block"
	internalErr = "fail_error"
	genBlock    = "block"
	empty       = "empty"
	labelEpoch  = "epoch"
)

var (
	blockGenCount = metrics.NewCounter(
		"generate",
		namespace,
		"number of block generation",
		[]string{"outcome"},
	)
	blockOkCnt     = blockGenCount.WithLabelValues(genBlock)
	emptyOutputCnt = blockGenCount.WithLabelValues(empty)
	failFetchCnt   = blockGenCount.WithLabelValues(failFetch)
	failGenCnt     = blockGenCount.WithLabelValues(failGen)
	failErrCnt     = blockGenCount.WithLabelValues(internalErr)
)

type collector struct {
	*Generator
	reg           *prometheus.Registry
	epochBlockCnt *prometheus.Desc
}

func newCollector(g *Generator) *collector {
	c := &collector{
		Generator: g,
		reg:       prometheus.NewRegistry(),
		epochBlockCnt: prometheus.NewDesc(
			prometheus.BuildFQName(metrics.Namespace, namespace, "block_counts"),
			"number of blocks created in each epoch",
			[]string{labelEpoch},
			nil,
		),
	}
	if err := prometheus.Register(c); err != nil {
		_, ok := err.(prometheus.AlreadyRegisteredError)
		if !ok {
			panic(err)
		}
	}
	return c
}

func (c *collector) Stop() {
	prometheus.Unregister(c)
}

func (c *collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.epochBlockCnt
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	for epoch, cnt := range c.BlockCounts() {
		ch <- prometheus.MustNewConstMetric(c.epochBlockCnt, prometheus.CounterValue, float64(cnt), epoch.String())
	}
}
