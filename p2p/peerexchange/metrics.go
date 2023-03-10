package peerexchange

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

const namespace = "discovery"

var (
	total = metrics.NewGauge(
		"total_addresses",
		namespace,
		"Total number of address in the book",
		[]string{},
	).WithLabelValues()
	connected = metrics.NewGauge(
		"connected_addresses",
		namespace,
		"Number of connected address in the book",
		[]string{},
	).WithLabelValues()
	buckets = metrics.NewGauge(
		"addresses_in_bucket",
		namespace,
		"Number of the address in the bucket",
		[]string{"bucket"},
	)
	private = buckets.WithLabelValues("private")
	public  = buckets.WithLabelValues("public")
	class   = metrics.NewGauge(
		"addresses_in_class",
		namespace,
		"Number of the address in the class",
		[]string{"class"},
	)
	stale   = class.WithLabelValues("stale")
	learned = class.WithLabelValues("learned")
	stable  = class.WithLabelValues("stable")
)

func newCollector(b *book.Book) *collector {
	c := &collector{b}
	if err := prometheus.Register(c); err != nil {
		panic(err)
	}
	return c
}

type collector struct {
	*book.Book
}

func (c *collector) Stop() {
	prometheus.Unregister(c)
}

func (c *collector) Describe(chan<- *prometheus.Desc) {
}

func (c *collector) Collect(chan<- prometheus.Metric) {
	stats := c.Book.Stats()
	total.Set(float64(stats.Total))
	connected.Set(float64(stats.Connected))
	private.Set(float64(stats.Private))
	public.Set(float64(stats.Public))
	stale.Set(float64(stats.Stale))
	learned.Set(float64(stats.Learned))
	stable.Set(float64(stats.Stable))
}
