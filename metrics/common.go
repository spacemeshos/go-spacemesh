package metrics

import (
	"github.com/go-kit/kit/metrics"
	prmkit "github.com/go-kit/kit/metrics/prometheus"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// Namespace is the basic namespace where all metrics are defined under.
	Namespace = "spacemesh"
	// ResultLabel makes a consistent name for results.
	ResultLabel = "result"
)

// Enabled variable is used to turn on or of the metrics server. when set to false nop metrics are used.
var Enabled = false

// Gauge is a metric type used to represent a numeric value
type Gauge metrics.Gauge

// Counter is a metric type used to represent a monotonically increased/decreased numeric value.
type Counter metrics.Counter

// Histogram is a metric type used to count multiple observations in buckets.
type Histogram metrics.Histogram

// Summary is a metric type used to sum up multiple observations over time.
type Summary prmkit.Summary

// NewCounter creates a Counter metrics under the global namespace returns nop if metrics are disabled.
func NewCounter(name, subsystem, help string, labels []string) Counter {
	if !Enabled {
		return nopCounter{}
	}
	return prmkit.NewCounterFrom(prometheus.CounterOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

// NewGauge creates a Gauge metrics under the global namespace returns nop if metrics are disabled.
func NewGauge(name, subsystem, help string, labels []string) Gauge {
	if !Enabled {
		return nopGauge{}
	}
	return prmkit.NewGaugeFrom(prometheus.GaugeOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

// NewHistogram creates a Histogram metrics under the global namespace returns nop if metrics are disabled.
func NewHistogram(name, subsystem, help string, labels []string) Histogram {
	if !Enabled {
		return nopHistogram{}
	}
	return prmkit.NewHistogramFrom(prometheus.HistogramOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help}, labels)
}

// NewSummary creates a Summary metrics under the global namespace returns nop if metrics are disabled.
func NewSummary(name, subsystem, help string, labels []string, objectives map[float64]float64) Histogram {
	if !Enabled {
		return nopSummary{}
	}
	// TODO github.com/go-kit/kit use Histogram instead of Summary
	return prmkit.NewSummaryFrom(prometheus.SummaryOpts{Namespace: Namespace, Subsystem: subsystem, Name: name, Help: help, Objectives: objectives}, labels)
}

// Cache is a basic cache interface that we can wrap to meter.
type Cache interface {
	Add(key, value interface{}) (evicted bool)
	Get(key interface{}) (value interface{}, ok bool)
}

// MeteredCache is a wrapper around a cache that monitors size, hits and misses.
type MeteredCache struct {
	hits  Counter
	miss  Counter
	count Gauge
	cache Cache
}

// NewMeteredCache wraps cache with metrics are returns a monitored cache.
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

// Add adds a key-value to the cache and increases the counter if needed.
func (m *MeteredCache) Add(key, value interface{}) bool {
	evicted := m.cache.Add(key, value)
	if !evicted {
		m.count.Add(1)
	}
	return evicted
}

// Get returns a value from the cache and counts a hit or a miss.
func (m *MeteredCache) Get(key interface{}) (value interface{}, ok bool) {
	v, k := m.cache.Get(key)
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

type nopSummary struct {
}

func (n nopSummary) With(labelValues ...string) metrics.Histogram {
	return n
}

func (n nopSummary) Observe(value float64) {
}
