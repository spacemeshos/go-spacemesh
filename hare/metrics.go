package hare

import (
	"fmt"
	"strings"
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

	msgDelayTracker = newMessageDelayTracker()
)

func newMessageDelayTracker() *messageDelayTracker {
	m := make(map[MessageType]map[bool]prometheus.Observer)
	for _, t := range []MessageType{pre, status, proposal, commit, notify} {
		m[t] = make(map[bool]prometheus.Observer)
		for _, pos := range []bool{true, false} {
			sign := "pos"
			if !pos {
				sign = "neg"
			}

			m[t][pos] = metrics.NewHistogramWithBuckets(
				fmt.Sprintf("%v_%v_delay_seconds", strings.ToLower(t.String()), sign),
				namespace,
				fmt.Sprintf("delta between expected time of receipt of %v message and actual receipt time", t),
				[]string{},
				prometheus.ExponentialBuckets(0.2, 2, 10),
			).WithLabelValues()
		}
	}
	return &messageDelayTracker{
		metrics: m,
	}
}

// messageDelayTracker provides a convenient interface to track delays for hare
// messages in prometheus histograms. The delay is calculated as the delta
// between the start of a round and the time a message in that round was
// received. There's a histogram for positive delays (i.e. the message arrived
// after the start of the round) which would be the only expected values if all
// nodes' clocks were synced. There's also a histogram for negative delays
// (i.e. the message arrived before the start of the round) in this case nodes'
// clocks must be out of sync. The gnerated metric names look like
// notify_neg_delay_seconds or notify_pos_delay_seconds with the first segment
// differing based on the message type.
type messageDelayTracker struct {
	metrics map[MessageType]map[bool]prometheus.Observer
}

func (m messageDelayTracker) trackDelay(t MessageType, clock RoundClock) {
	subMap, ok := m.metrics[t]
	if !ok {
		// Untracked message type
		return
	}
	seconds := time.Since(clock.RoundEnd(t.round() - 1)).Seconds()
	metric := subMap[seconds >= 0]
	metric.Observe(float64(seconds))
}
