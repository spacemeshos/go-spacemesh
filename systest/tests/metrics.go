package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/prometheus/common/expfmt"
)

var errMetricNotFound = errors.New("metric not found")

func fetchCounterMetric(
	ctx context.Context,
	url string,
	metricName string,
	labelFilters map[string]string,
) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create HTTP request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch metrics: %v", err)
	}
	defer resp.Body.Close()

	parser := expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to parse metric families: %v", err)
	}

	metricFamily, ok := metricFamilies[metricName]
	if !ok {
		return 0, fmt.Errorf("metric not found: %s", metricName)
	}

	for _, metric := range metricFamily.Metric {
		if metric.Counter != nil {
			// Check if the metric has the specified labels
			labels := make(map[string]string)
			for _, lp := range metric.Label {
				labels[lp.GetName()] = lp.GetValue()
			}

			matches := true
			for key, value := range labelFilters {
				if labels[key] != value {
					matches = false
					break
				}
			}

			if matches {
				return metric.GetCounter().GetValue(), nil
			}
		}
	}

	return 0, errMetricNotFound
}
