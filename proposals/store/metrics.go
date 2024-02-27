package store

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const subsystem = "proposals"

var numProposals = metrics.NewCounter(
	"proposals_in_layer",
	subsystem,
	"number of proposals in layer",
	[]string{"layer"},
)
