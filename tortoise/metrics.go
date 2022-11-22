package tortoise

import "github.com/spacemeshos/go-spacemesh/metrics"

const namespace = "tortoise"

var (
	ballotsNumber = metrics.NewGauge(
		"ballots",
		namespace,
		"Number of ballots in the state",
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
