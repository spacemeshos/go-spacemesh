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
	*Certifier
	epochCertCount *prometheus.Desc
}

func newCollector(certifier *Certifier) *collector {
	c := &collector{
		Certifier: certifier,
		epochCertCount: prometheus.NewDesc(
			prometheus.BuildFQName(metrics.Namespace, namespace, "cert_count"),
			"number of certificate created/synced in each epoch",
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
	ch <- c.epochCertCount
}

func (c *collector) Collect(ch chan<- prometheus.Metric) {
	for epoch, cnt := range c.CertCount() {
		ch <- prometheus.MustNewConstMetric(c.epochCertCount, prometheus.CounterValue, float64(cnt), epoch.String())
	}
}
