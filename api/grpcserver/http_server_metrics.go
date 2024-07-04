package grpcserver

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	httpMetrics "github.com/slok/go-http-metrics/metrics"
	metricsProm "github.com/slok/go-http-metrics/metrics/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

type httpMetricsRecorder struct {
	httpRequestDurHistogram   *prometheus.HistogramVec
	httpResponseSizeHistogram *prometheus.HistogramVec
	httpRequestsInflight      *prometheus.GaugeVec
}

var metricsRecorder *httpMetrics.Recorder

// newMetricsRecorder returns a new metrics recorder that implements the recorder
// using Prometheus as the backend.
func newMetricsRecorder(cfg metricsProm.Config) httpMetrics.Recorder {
	r := &httpMetricsRecorder{
		httpRequestDurHistogram: metrics.NewHistogramWithBuckets(
			"api_request_duration_seconds",
			metrics.Namespace,
			"The latency of the HTTP requests.",
			[]string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel},
			cfg.DurationBuckets,
		),

		httpResponseSizeHistogram: metrics.NewHistogramWithBuckets(
			"api_response_size_bytes",
			metrics.Namespace,
			"The size of the HTTP responses.",
			[]string{cfg.ServiceLabel, cfg.HandlerIDLabel, cfg.MethodLabel, cfg.StatusCodeLabel},
			cfg.SizeBuckets,
		),

		httpRequestsInflight: metrics.NewGauge(
			"api_requests_inflight",
			metrics.Namespace,
			"The number of inflight requests being handled at the same time.",
			[]string{cfg.ServiceLabel, cfg.HandlerIDLabel},
		),
	}

	return r
}

func (r httpMetricsRecorder) ObserveHTTPRequestDuration(_ context.Context, p httpMetrics.HTTPReqProperties,
	duration time.Duration,
) {
	r.httpRequestDurHistogram.WithLabelValues(p.Service, p.ID, p.Method, p.Code).Observe(duration.Seconds())
}

func (r httpMetricsRecorder) ObserveHTTPResponseSize(_ context.Context, p httpMetrics.HTTPReqProperties,
	sizeBytes int64,
) {
	r.httpResponseSizeHistogram.WithLabelValues(p.Service, p.ID, p.Method, p.Code).Observe(float64(sizeBytes))
}

func (r httpMetricsRecorder) AddInflightRequests(_ context.Context, p httpMetrics.HTTPProperties, quantity int) {
	r.httpRequestsInflight.WithLabelValues(p.Service, p.ID).Add(float64(quantity))
}
