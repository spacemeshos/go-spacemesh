package proposals

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/metrics"
)

const (
	subsystem = "proposals"

	// diffTypeLabel is the label name for the type of different opinions with base block.
	diffTypeLabel = "diff_type"

	// diffTypeFor is the label value for different opinions with base ballot when voting for blocks.
	diffTypeFor = "diff_for"
	// diffTypeAgainst is the label value for different opinions with base ballot when voting against blocks.
	diffTypeAgainst = "diff_against"
	// diffTypeNeutral is the label value for different opinions with base ballot when voting neutral on blocks.
	diffTypeNeutral = "diff_neutral"
)

// proposalSize records average size of proposals.
var proposalSize = metrics.NewHistogramWithBuckets(
	"proposal_size",
	subsystem,
	"proposal size in bytes",
	[]string{},
	prometheus.ExponentialBuckets(100, 2, 8),
)

// numTxsInProposal records average number of transactions in a proposal.
var numTxsInProposal = metrics.NewHistogramWithBuckets(
	"num_txs_in_proposal",
	subsystem,
	"number of transactions in proposal",
	[]string{},
	prometheus.ExponentialBuckets(1, 2, 8),
)

// numBlocksInException records the number of blocks encoded in a ballot for a given exception type (against, for, and neutral).
var numBlocksInException = metrics.NewHistogramWithBuckets(
	"num_blocks_in_exception",
	subsystem,
	"number of blocks in an exception list",
	[]string{
		diffTypeLabel,
	},
	prometheus.ExponentialBuckets(1, 2, 8),
)

var (
	proposalDuration = metrics.NewHistogramWithBuckets(
		"proposal_duration",
		subsystem,
		"Duration in ns for processing a proposal",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	proposalDurationDecodeInit = metrics.NewHistogramWithBuckets(
		"proposal_duration_decode_init",
		subsystem,
		"Duration in ns for decoding/initializing a proposal",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	proposalDurationTXs = metrics.NewHistogramWithBuckets(
		"proposal_duration_txs",
		subsystem,
		"Duration in ns for fetching txs in a proposal",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	proposalDurationDBLookup = metrics.NewHistogramWithBuckets(
		"proposal_duration_db_lookup",
		subsystem,
		"Duration in ns for looking up a proposal in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	proposalDurationDBSave = metrics.NewHistogramWithBuckets(
		"proposal_duration_db_save",
		subsystem,
		"Duration in ns for saving a proposal in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	ballotDuration = metrics.NewHistogramWithBuckets(
		"ballot_duration",
		subsystem,
		"Duration in ns for processing a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	ballotDurationDataAvailability = metrics.NewHistogramWithBuckets(
		"ballot_duration_data_available",
		subsystem,
		"Duration in ns for checking data availability in a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	ballotDurationVotesConsistency = metrics.NewHistogramWithBuckets(
		"ballot_duration_data_consistent",
		subsystem,
		"Duration in ns for checking data consistency in a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	ballotDurationEligibility = metrics.NewHistogramWithBuckets(
		"ballot_duration_eligibility",
		subsystem,
		"Duration in ns for checking eligibility of a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	ballotDurationDBLookup = metrics.NewHistogramWithBuckets(
		"ballot_duration_db_lookup",
		subsystem,
		"Duration in ns for looking up a ballot in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	ballotDurationDBSave = metrics.NewHistogramWithBuckets(
		"ballot_duration_db_save",
		subsystem,
		"Duration in ns for saving up a ballot in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
)
