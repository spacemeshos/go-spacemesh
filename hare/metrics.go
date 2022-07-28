package hare

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "hare"

	// labels for hare consensus output.
	success = "ok"
	failure = "fail"

	// labels for block generation.
	failFetch   = "fail_proposal"
	failGen     = "fail_block"
	internalErr = "fail_error"
	genBlock    = "block"
	empty       = "empty"
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

	blockGenCount = metrics.NewCounter(
		"block",
		namespace,
		"number of block generation",
		[]string{"outcome"},
	)
	blockOkCnt     = blockGenCount.WithLabelValues(genBlock)
	emptyOutputCnt = blockGenCount.WithLabelValues(empty)
	failFetchCnt   = blockGenCount.WithLabelValues(failFetch)
	failGenCnt     = blockGenCount.WithLabelValues(failGen)
	failErrCnt     = blockGenCount.WithLabelValues(internalErr)

	numIterations = metrics.NewHistogramWithBuckets(
		"num_iterations",
		namespace,
		"number of iterations for hare to terminate",
		[]string{},
		prometheus.ExponentialBuckets(4, 2, 3),
	).WithLabelValues()
)
