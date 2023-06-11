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

const (
	// labels for each step of proposal/ballot processing.
	decodeInit = "decode"
	dbLookup   = "db_r"
	peerHashes = "hashes"
	ballot     = "ballot"
	fetchTXs   = "txs"
	dbSave     = "db_w"
	linkTxs    = "link"

	dataCheck = "data"   // check data integrity
	fetchRef  = "fetch"  // check referenced ballots/atxs/blocks are available
	decode    = "decode" // decoding ballot using tortoise
	votes     = "votes"  // check votes are consistent
	eligible  = "elig"   // check ballot eligibility
)

var (
	proposalDuration = metrics.NewHistogramWithBuckets(
		"proposal_duration",
		subsystem,
		"Duration in ns for processing a proposal",
		[]string{"step"},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	)
	ballotDuration = metrics.NewHistogramWithBuckets(
		"ballot_duration",
		subsystem,
		"Duration in ns for processing a ballot",
		[]string{"step"},
		prometheus.ExponentialBuckets(1_000_000, 4, 10),
	)
)

var (
	processErrors = metrics.NewCounter(
		"errs",
		subsystem,
		"number of errors",
		[]string{"kind"},
	)
	malformed      = processErrors.WithLabelValues("mal")
	failedInit     = processErrors.WithLabelValues("init")
	known          = processErrors.WithLabelValues("known")
	preGenesis     = processErrors.WithLabelValues("genesis")
	badSigProposal = processErrors.WithLabelValues("sigp")
	badSigBallot   = processErrors.WithLabelValues("sigb")
	badData        = processErrors.WithLabelValues("data")
	unavailRef     = processErrors.WithLabelValues("avail")
	badVote        = processErrors.WithLabelValues("vote")
	notEligible    = processErrors.WithLabelValues("elig")
	failedPublish  = processErrors.WithLabelValues("pub")
)
