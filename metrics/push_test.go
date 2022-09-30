package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"
)

func TestStartPushMetrics(t *testing.T) {
	testMetricName := "testMetric"

	testMetric := promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Subsystem: "Tests",
		Name:      testMetricName,
		Help:      "Should be equal 1",
	}, nil)
	testMetric.WithLabelValues().Add(1)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}

		res := string(resBytes)
		if !strings.Contains(res, testMetricName+" 1") {
			t.Fatal("r.Body doesn't contains out test metric!")
		}

		w.WriteHeader(202)
	}))
	defer ts.Close()

	pusher := push.New(ts.URL, "my_job").
		Gatherer(prometheus.DefaultGatherer).
		Format(expfmt.FmtText)
	err := pusher.Push()
	if err != nil {
		t.Fatal("can't push to server", err)
	}
}
