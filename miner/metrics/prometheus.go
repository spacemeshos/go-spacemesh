package metrics

import (
	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	// subsystem is a subsystem shared by all metrics exposed by this package.
	subsystem = "miner"
)

// ProposalBuildDuration checks duration to build a proposal in milliseconds.
var ProposalBuildDuration = metrics.NewHistogramWithBuckets(
	"proposal_build_duration",
	subsystem,
	"duration to build a proposal in milliseconds",
	[]string{},
	[]float64{10, 100, 1000, 5 * 1000, 10 * 1000, 60 * 1000, 10 * 60 * 1000, 60 * 60 * 1000},
)
