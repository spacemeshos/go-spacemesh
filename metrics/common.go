package metrics

import (
	"github.com/go-kit/kit/metrics"
	prmkit "github.com/go-kit/kit/metrics/prometheus"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	Namespace   = "spacemesh"
	ResultLabel = "result"
)

var Enabled = false

type Gauge metrics.Gauge
type Counter metrics.Counter
type Histogram metrics.Histogram

func NewCounter(name, subsystem, help string, labels []string) Counter {
	if !Enabled {
		return nopCounter{}
	}
	return prmkit.NewCounterFrom(prometheus.CounterOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

func NewGauge(name, subsystem, help string, labels []string) Gauge {
	if !Enabled {
		return nopGauge{}
	}
	return prmkit.NewGaugeFrom(prometheus.GaugeOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

func NewHistogram(name, subsystem, help string, labels []string) Histogram {
	if !Enabled {
		return nopHistogram{}
	}
	return prmkit.NewHistogramFrom(prometheus.HistogramOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

type Cache interface {
	Add(key, value interface{}) (evicted bool)
	Get(key interface{}) (value interface{}, ok bool)
}

type MeteredCache struct {
	hits  Counter
	miss  Counter
	count Gauge
	cache Cache
}

func NewMeteredCache(cache Cache, subsystem, name, help string, labels []string) *MeteredCache {
	total := NewCounter(name+"_queries", subsystem, help, labels)
	hits := total.With(ResultLabel, "hit")
	miss := total.With(ResultLabel, "miss")
	count := NewGauge(name+"_count", subsystem, help, labels)
	return &MeteredCache{
		hits,
		miss,
		count,
		cache,
	}
}

func (m *MeteredCache) Add(key, value interface{}) bool {
	evicted := m.cache.Add(key, value)
	if !evicted {
		m.count.Add(1)
	}
	return evicted
}

func (m *MeteredCache) Get(key interface{}) (value interface{}, ok bool) {
	v, k := m.Get(key)
	if k {
		m.hits.Add(1)
	} else {
		m.miss.Add(1)
	}
	return v, k
}

// Nops

type nopCounter struct {
}

func (n nopCounter) With(labelValues ...string) metrics.Counter {
	return n
}

func (n nopCounter) Add(value float64) {

}

type nopGauge struct {
}

func (n nopGauge) With(labelValues ...string) metrics.Gauge {
	return n
}

func (n nopGauge) Set(value float64) {

}

func (n nopGauge) Add(value float64) {

}

type nopHistogram struct {
}

func (n nopHistogram) With(labelValues ...string) metrics.Histogram {
	return n
}

func (n nopHistogram) Observe(value float64) {

}
