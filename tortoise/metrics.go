package tortoise

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const namespace = "tortoise"

var (
	ballotsNumber = metrics.NewGauge(
		"ballots",
		namespace,
		"Number of ballots in the state",
		[]string{},
	).WithLabelValues()
	delayedBallots = metrics.NewGauge(
		"delayed_ballots",
		namespace,
		"Number of ballots that are delayed due to the wrong beacon",
		[]string{},
	).WithLabelValues()
	blocksNumber = metrics.NewGauge(
		"blocks",
		namespace,
		"Number of blocks in the state",
		[]string{},
	).WithLabelValues()
	layersNumber = metrics.NewGauge(
		"layers",
		namespace,
		"Number of layers in the state",
		[]string{},
	).WithLabelValues()
	epochsNumber = metrics.NewGauge(
		"epochs",
		namespace,
		"Number of epochs in the state",
		[]string{},
	).WithLabelValues()
)

var (
	onBallotHist = metrics.NewHistogramWithBuckets(
		"tortoise_on_ballot_ns",
		namespace,
		"Time to process ballot in ns.",
		[]string{"step"},
		prometheus.ExponentialBuckets(100_000, 2, 10),
	)
	waitBallotDuration   = onBallotHist.WithLabelValues("wait")
	decodeBallotDuration = onBallotHist.WithLabelValues("decode")
	fcountBallotDuration = onBallotHist.WithLabelValues("full_count")
	vcountBallotDuration = onBallotHist.WithLabelValues("verifying_count")
)

var (
	onBlockHist = metrics.NewHistogramWithBuckets(
		"tortoise_on_block_ns",
		namespace,
		"Time to process block in ns.",
		[]string{"step"},
		prometheus.ExponentialBuckets(100_000, 2, 10),
	)
	waitBlockDuration = onBlockHist.WithLabelValues("wait")
	addBlockDuration  = onBlockHist.WithLabelValues("add")
	lateBlockDuration = onBlockHist.WithLabelValues("late")
)

var (
	onHareOutputHist = metrics.NewHistogramWithBuckets(
		"tortoise_on_hare_output_ns",
		namespace,
		"Time to process hare output in ns.",
		[]string{"step"},
		prometheus.ExponentialBuckets(100_000, 2, 10),
	)
	waitHareOutputDuration = onHareOutputHist.WithLabelValues("wait")
)
