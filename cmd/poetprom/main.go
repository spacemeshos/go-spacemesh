package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/prometheus/common/expfmt"
)

func fetchCounterMetric(url, metricName string, labelFilters map[string]string) (float64, error) {
	resp, err := http.Get(url)
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

	// Find the metric value with matching labels
	for _, metric := range metricFamily.Metric {
		if metric.GetCounter() == nil {
			continue
		}
		labels := make(map[string]string, len(metric.Label))
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

	return 0, fmt.Errorf("counter metric value not found for metric: %s", metricName)
}

func main() {
	// Define the Prometheus metrics URL
	prometheusURL := "http://localhost:8081/metrics"

	// Define the metric name to fetch
	metricName := "poet_registration_with_cert_total"

	// Define label filters (change as needed)
	labelFilters := map[string]string{
		"result": "invalid",
	}

	// Fetch the counter metric value with label filtering
	metricValue, err := fetchCounterMetric(prometheusURL, metricName, labelFilters)
	if err != nil {
		log.Fatalf("Error fetching counter metric %s: %v", metricName, err)
	}

	// Print the counter metric value
	fmt.Printf("Counter Metric %s value with label filtering: %f\n", metricName, metricValue)
}
