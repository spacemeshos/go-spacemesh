package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/require"
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
		require.NoError(t, err)

		res := string(resBytes)
		require.Contains(t, res, testMetricName+" 1")

		w.WriteHeader(202)
	}))
	defer ts.Close()

	pusher := push.New(ts.URL, "my_job").
		Gatherer(prometheus.DefaultGatherer).
		Format(expfmt.NewFormat(expfmt.TypeTextPlain))
	require.NoError(t, pusher.Push())
}
