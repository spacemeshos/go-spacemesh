package grpcserver

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/slok/go-http-metrics/metrics"
	metricsProm "github.com/slok/go-http-metrics/metrics/prometheus"
)

type metricsRecorder struct {
	httpRequestDurHistogram   *prometheus.HistogramVec
	httpResponseSizeHistogram *prometheus.HistogramVec
	httpRequestsInflight      *prometheus.GaugeVec
}

// newMetricsRecorder returns a new metrics recorder that implements the recorder
// using Prometheus as the backend.
func newMetricsRecorder(cfg metricsProm.Config) metrics.Recorder {
	r := &metricsRecorder{
		httpRequestDurHistogram: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "The latency of the HTTP requests.",
			Buckets:   cfg.DurationBuckets,
		}, []string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel}),

		httpResponseSizeHistogram: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "response_size_bytes",
			Help:      "The size of the HTTP responses.",
			Buckets:   cfg.SizeBuckets,
		}, []string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel}),

		httpRequestsInflight: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: cfg.Prefix,
			Subsystem: "http",
			Name:      "requests_inflight",
			Help:      "The number of inflight requests being handled at the same time.",
		}, []string{cfg.ServiceLabel, cfg.HandlerIDLabel}),
	}

	return r
}

func (r metricsRecorder) ObserveHTTPRequestDuration(_ context.Context, p metrics.HTTPReqProperties,
	duration time.Duration,
) {
	r.httpRequestDurHistogram.WithLabelValues(p.Service, p.ID, p.Method, p.Code).Observe(duration.Seconds())
}

func (r metricsRecorder) ObserveHTTPResponseSize(_ context.Context, p metrics.HTTPReqProperties, sizeBytes int64) {
	r.httpResponseSizeHistogram.WithLabelValues(p.Service, p.ID, p.Method, p.Code).Observe(float64(sizeBytes))
}

func (r metricsRecorder) AddInflightRequests(_ context.Context, p metrics.HTTPProperties, quantity int) {
	r.httpRequestsInflight.WithLabelValues(p.Service, p.ID).Add(float64(quantity))
}
