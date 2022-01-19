package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem = "proposals"

	// DiffTypeLabel is the label name for the type of different opinions with base block.
	DiffTypeLabel = "diff_type"

	// DiffTypeFor is the label value for different opinions with base ballot when voting for blocks.
	DiffTypeFor = "diff_for"
	// DiffTypeAgainst is the label value for different opinions with base ballot when voting against blocks.
	DiffTypeAgainst = "diff_against"
	// DiffTypeNeutral is the label value for different opinions with base ballot when voting neutral on blocks.
	DiffTypeNeutral = "diff_neutral"
)

// ProposalSize records average size of proposals.
var ProposalSize = metrics.NewHistogramWithBuckets(
	"proposal_size",
	subsystem,
	"proposal size in bytes",
	[]string{},
	prometheus.ExponentialBuckets(100, 2, 8),
)

// NumTxsInProposal records average number of transactions in a proposal.
var NumTxsInProposal = metrics.NewHistogramWithBuckets(
	"num_txs_in_proposal",
	subsystem,
	"number of transactions in proposal",
	[]string{},
	prometheus.ExponentialBuckets(1, 2, 8),
)

// NumBlocksInException records the number of blocks encoded in a ballot for a given exception type (against, for, and neutral).
var NumBlocksInException = metrics.NewHistogramWithBuckets(
	"num_blocks_in_exception",
	subsystem,
	"number of blocks in an exception list",
	[]string{
		DiffTypeLabel,
	},
	prometheus.ExponentialBuckets(1, 2, 8),
)
