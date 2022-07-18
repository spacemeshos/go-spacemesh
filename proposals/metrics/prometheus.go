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

var (
	// ProposalDuration ...
	ProposalDuration = metrics.NewHistogramWithBuckets(
		"proposal_duration",
		subsystem,
		"Duration in ns for processing a proposal",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// ProposalDurationDecodeInit ...
	ProposalDurationDecodeInit = metrics.NewHistogramWithBuckets(
		"proposal_duration_decode_init",
		subsystem,
		"Duration in ns for decoding/initializing a proposal",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// ProposalDurationTXs ...
	ProposalDurationTXs = metrics.NewHistogramWithBuckets(
		"proposal_duration_txs",
		subsystem,
		"Duration in ns for fetching txs in a proposal",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// ProposalDurationDBLookup ...
	ProposalDurationDBLookup = metrics.NewHistogramWithBuckets(
		"proposal_duration_db_lookup",
		subsystem,
		"Duration in ns for looking up a proposal in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// ProposalDurationDBSave ...
	ProposalDurationDBSave = metrics.NewHistogramWithBuckets(
		"proposal_duration_db_save",
		subsystem,
		"Duration in ns for saving a proposal in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// BallotDuration ...
	BallotDuration = metrics.NewHistogramWithBuckets(
		"ballot_duration",
		subsystem,
		"Duration in ns for processing a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// BallotDurationDataAvailability ...
	BallotDurationDataAvailability = metrics.NewHistogramWithBuckets(
		"ballot_duration_data_available",
		subsystem,
		"Duration in ns for checking data availability in a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// BallotDurationVotesConsistency ...
	BallotDurationVotesConsistency = metrics.NewHistogramWithBuckets(
		"ballot_duration_data_consistent",
		subsystem,
		"Duration in ns for checking data consistency in a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// BallotDurationEligibility ...
	BallotDurationEligibility = metrics.NewHistogramWithBuckets(
		"ballot_duration_eligibility",
		subsystem,
		"Duration in ns for checking eligibility of a ballot",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// BallotDurationDBLookup ...
	BallotDurationDBLookup = metrics.NewHistogramWithBuckets(
		"ballot_duration_db_lookup",
		subsystem,
		"Duration in ns for looking up a ballot in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
	// BallotDurationDBSave ...
	BallotDurationDBSave = metrics.NewHistogramWithBuckets(
		"ballot_duration_db_save",
		subsystem,
		"Duration in ns for saving up a ballot in db",
		[]string{},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	).WithLabelValues()
)
