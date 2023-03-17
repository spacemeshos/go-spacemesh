package hare

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "hare"

	// labels for hare consensus output.
	success = "ok"
	failure = "fail"
)

var (
	preNumProposals = metrics.NewCounter(
		"in_proposals",
		namespace,
		"number of proposals for as consensus input",
		[]string{},
	).WithLabelValues()

	postNumProposals = metrics.NewCounter(
		"out_proposals",
		namespace,
		"number of proposals for as consensus output",
		[]string{},
	).WithLabelValues()

	consensusCount = metrics.NewCounter(
		"consensus",
		namespace,
		"number of hare consensus",
		[]string{"outcome"},
	)
	consensusOkCnt   = consensusCount.WithLabelValues(success)
	consensusFailCnt = consensusCount.WithLabelValues(failure)

	numIterations = metrics.NewHistogramWithBuckets(
		"num_iterations",
		namespace,
		"number of iterations for hare to terminate",
		[]string{},
		prometheus.ExponentialBuckets(4, 2, 3),
	).WithLabelValues()

	hareLatency = metrics.NewHistogramWithBuckets(
		"hare_latency_seconds",
		namespace,
		"Observed latency for hare message",
		[]string{"type", "sign"},
		prometheus.ExponentialBuckets(0.1, 2, 12),
	)
)

// reportLatency provides a convenient interface to track latency for hare
// messages in a prometheus histogram. The delay is calculated as the delta
// between the start of a round and the time a message in that round was
// actually received. There's a label per message type and there are also
// positive and negative labels to track early and late messages. Early
// messages are ones that arrive before the round has started and late messages
// arrive after the round has started. In a properly functioning system all
// messages should be late. Early messages indicate a lack of time
// synchronization between nodes.
func reportLatency(t MessageType, round uint32, clock RoundClock) {
	seconds := time.Since(clock.RoundEnd(round - 1)).Seconds()
	sign := "pos"
	if seconds < 0 {
		sign = "neg"
		// If the observation is negative make it positive.
		seconds -= seconds
	}
	hareLatency.WithLabelValues(t.String(), sign).Observe(seconds)
}
