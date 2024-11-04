package blocks

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	namespace = "blocks"

	// Labels for block generation.
	failFetch   = "fail_proposal"
	failGen     = "fail_block"
	internalErr = "fail_error"
	genBlock    = "block"
	empty       = "empty"
	labelEpoch  = "epoch"
)

var (
	blockGenCount = metrics.NewCounter(
		"generate",
		namespace,
		"number of block generation",
		[]string{"outcome"},
	)
	blockOkCnt     = blockGenCount.WithLabelValues(genBlock)
	emptyOutputCnt = blockGenCount.WithLabelValues(empty)
	failFetchCnt   = blockGenCount.WithLabelValues(failFetch)
	failGenCnt     = blockGenCount.WithLabelValues(failGen)
	failErrCnt     = blockGenCount.WithLabelValues(internalErr)
)
