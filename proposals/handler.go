package proposals

import (
	"context"
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/metrics"
	"github.com/spacemeshos/go-spacemesh/system"
)

// NewProposalProtocol is the protocol indicator for gossip Proposals.
const NewProposalProtocol = "newProposal"

var (
	errMalformedData         = errors.New("malformed data")
	errInitialize            = errors.New("failed to initialize")
	errInvalidATXID          = errors.New("ballot has invalid ATXID")
	errMissingBaseBallot     = errors.New("base ballot is missing")
	errMissingEpochData      = errors.New("epoch data is missing in ref ballot")
	errUnexpectedEpochData   = errors.New("non-ref ballot declares epoch data")
	errEmptyActiveSet        = errors.New("ref ballot declares empty active set")
	errMissingBeacon         = errors.New("beacon is missing in ref ballot")
	errNotEligible           = errors.New("ballot not eligible")
	errConflictingExceptions = errors.New("conflicting exceptions")
	errExceptionsOverflow    = errors.New("too many exceptions")
	errDuplicateTX           = errors.New("duplicate TxID in proposal")
	errDuplicateATX          = errors.New("duplicate ATXID in active set")
	errKnownBallot           = errors.New("known ballot")
)

// Handler processes Proposal from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger log.Log
	cfg    Config

	fetcher    system.Fetcher
	mesh       meshDB
	proposalDB proposalDB
	validator  eligibilityValidator
}

// Config defines configuration for the handler.
type Config struct {
	LayerSize            uint32
	LayersPerEpoch       uint32
	GoldenATXID          types.ATXID
	MaxExceptions        int
	EligibilityValidator eligibilityValidator
}

// defaultConfig for BlockHandler.
func defaultConfig() Config {
	return Config{
		MaxExceptions:        1000,
		EligibilityValidator: nil,
	}
}

// Opt for configuring Handler.
type Opt func(h *Handler)

// WithGoldenATXID defines the golden ATXID.
func WithGoldenATXID(atx types.ATXID) Opt {
	return func(h *Handler) {
		h.cfg.GoldenATXID = atx
	}
}

// WithLayerSize defines the average number of proposal per layer.
func WithLayerSize(size uint32) Opt {
	return func(h *Handler) {
		h.cfg.LayerSize = size
	}
}

// WithLayerPerEpoch defines the number of layers per epoch.
func WithLayerPerEpoch(layers uint32) Opt {
	return func(h *Handler) {
		h.cfg.LayersPerEpoch = layers
	}
}

// withValidator defines eligibility Validator.
func withValidator(v eligibilityValidator) Opt {
	return func(h *Handler) {
		h.validator = v
	}
}

// WithMaxExceptions defines max allowed exceptions in a ballot.
func WithMaxExceptions(max int) Opt {
	return func(h *Handler) {
		h.cfg.MaxExceptions = max
	}
}

// WithLogger defines logger for Handler.
func WithLogger(logger log.Log) Opt {
	return func(h *Handler) {
		h.logger = logger
	}
}

// NewHandler creates new Handler.
func NewHandler(f system.Fetcher, bc system.BeaconCollector, db atxDB, m meshDB, p proposalDB, opts ...Opt) *Handler {
	b := &Handler{
		logger:     log.NewNop(),
		cfg:        defaultConfig(),
		fetcher:    f,
		mesh:       m,
		proposalDB: p,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.validator == nil {
		b.validator = NewEligibilityValidator(b.cfg.LayerSize, b.cfg.LayersPerEpoch, db, bc, m, b.logger)
	}
	return b
}

// HandleProposal is the gossip receiver for Proposal.
func (h *Handler) HandleProposal(ctx context.Context, _ peer.ID, msg []byte) pubsub.ValidationResult {
	newCtx := log.WithNewRequestID(ctx)
	if err := h.HandleProposalData(newCtx, msg); err != nil {
		h.logger.WithContext(newCtx).With().Error("failed to process proposal gossip", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

// HandleBlockData handles Block data from sync.
func (h *Handler) HandleBlockData(ctx context.Context, data []byte) error {
	newCtx := log.WithNewRequestID(ctx)
	return h.HandleProposalData(newCtx, data)
}

// HandleBallotData handles Ballot data from gossip and sync.
func (h *Handler) HandleBallotData(ctx context.Context, data []byte) error {
	newCtx := log.WithNewRequestID(ctx)
	logger := h.logger.WithContext(newCtx)
	logger.Info("processing ballot")

	var b types.Ballot
	if err := codec.Decode(data, &b); err != nil {
		logger.With().Error("malformed ballot", log.Err(err))
		return errMalformedData
	}

	// set the ballot and smesher ID when received
	if err := b.Initialize(); err != nil {
		logger.With().Error("failed to initialize ballot", log.Err(err))
		return errInitialize
	}

	logger = logger.WithFields(b.ID(), b.LayerIndex)
	err := h.processBallot(ctx, &b, logger)
	if err != nil && err != errKnownBallot {
		return err
	} else if err == errKnownBallot {
		return nil
	}
	if err = h.mesh.AddBallot(&b); err != nil {
		return fmt.Errorf("save ballot: %w", err)
	}
	return nil
}

// HandleProposalData handles Proposal data from sync.
func (h *Handler) HandleProposalData(ctx context.Context, data []byte) error {
	logger := h.logger.WithContext(ctx)
	logger.Info("processing proposal")

	var p types.Proposal
	if err := codec.Decode(data, &p); err != nil {
		logger.With().Error("malformed proposal", log.Err(err))
		return errMalformedData
	}

	// set the proposal ID when received
	if err := p.Initialize(); err != nil {
		logger.With().Warning("failed to initialize proposal", log.Err(err))
		return errInitialize
	}

	logger = logger.WithFields(p.ID(), p.Ballot.ID(), p.LayerIndex)

	if h.proposalDB.HasProposal(p.ID()) {
		logger.Info("known proposal")
		return nil
	}
	logger.With().Info("new proposal", p.Fields()...)

	if err := h.processBallot(ctx, &p.Ballot, logger); err != nil && err != errKnownBallot {
		logger.With().Warning("failed to process ballot", log.Err(err))
		return err
	}

	if err := h.checkTransactions(ctx, &p); err != nil {
		logger.With().Warning("failed to fetch proposal TXs", log.Err(err))
		return err
	}

	h.logger.WithContext(ctx).With().Debug("proposal is syntactically valid")
	reportProposalMetrics(&p)

	if err := h.proposalDB.AddProposalWithTxs(ctx, &p); err != nil {
		logger.With().Error("failed to save proposal", log.Err(err))
		return fmt.Errorf("save proposal: %w", err)
	}

	return nil
}

func (h *Handler) processBallot(ctx context.Context, b *types.Ballot, logger log.Log) error {
	if h.mesh.HasBallot(b.ID()) {
		logger.Info("known ballot")
		return errKnownBallot
	}
	logger.With().Info("new ballot", log.Object("ballot", b))

	if err := h.checkBallotSyntacticValidity(ctx, b); err != nil {
		logger.With().Error("ballot syntactically invalid", log.Err(err))
		return fmt.Errorf("syntactic-check ballot: %w", err)
	}

	reportBallotMetrics(b)
	return nil
}

func (h *Handler) checkBallotSyntacticValidity(ctx context.Context, b *types.Ballot) error {
	h.logger.WithContext(ctx).With().Debug("checking proposal syntactic validity")

	if err := h.checkBallotDataIntegrity(b); err != nil {
		h.logger.WithContext(ctx).With().Warning("ballot integrity check failed", log.Err(err))
		return err
	}

	if err := h.checkBallotDataAvailability(ctx, b); err != nil {
		h.logger.WithContext(ctx).With().Warning("ballot data availability check failed", log.Err(err))
		return err
	}

	if eligible, err := h.validator.CheckEligibility(ctx, b); err != nil || !eligible {
		h.logger.WithContext(ctx).With().Warning("ballot eligibility check failed", log.Err(err))
		return errNotEligible
	}

	h.logger.WithContext(ctx).With().Debug("ballot is syntactically valid")
	return nil
}

func (h *Handler) checkBallotDataIntegrity(b *types.Ballot) error {
	if b.AtxID == *types.EmptyATXID || b.AtxID == h.cfg.GoldenATXID {
		return errInvalidATXID
	}

	if b.BaseBallot == types.EmptyBallotID {
		return errMissingBaseBallot
	}

	if b.RefBallot == types.EmptyBallotID {
		// this is the smesher's first Ballot in this epoch, should contain EpochData
		if b.EpochData == nil {
			return errMissingEpochData
		}
		if b.EpochData.Beacon == types.EmptyBeacon {
			return errMissingBeacon
		}
		if len(b.EpochData.ActiveSet) == 0 {
			return errEmptyActiveSet
		}
		// check for duplicate ATXIDs in active set
		set := make(map[types.ATXID]struct{}, len(b.EpochData.ActiveSet))
		for _, atx := range b.EpochData.ActiveSet {
			if _, exist := set[atx]; exist {
				return errDuplicateATX
			}
			set[atx] = struct{}{}
		}
	} else {
		if b.EpochData != nil {
			return errUnexpectedEpochData
		}
	}

	if err := checkExceptions(b, h.cfg.MaxExceptions); err != nil {
		return err
	}
	return nil
}

func checkExceptions(ballot *types.Ballot, max int) error {
	exceptions := map[types.BlockID]struct{}{}
	for _, diff := range [][]types.BlockID{ballot.ForDiff, ballot.NeutralDiff, ballot.AgainstDiff} {
		for _, bid := range diff {
			_, exist := exceptions[bid]
			if exist {
				return fmt.Errorf("%w: block %s is referenced multiple times in exceptions of ballot %s",
					errConflictingExceptions, bid, ballot.ID())
			}
			exceptions[bid] = struct{}{}
		}
	}
	if len(exceptions) > max {
		return fmt.Errorf("%w: %d exceptions with max allowed %d in ballot %s",
			errExceptionsOverflow, len(exceptions), max, ballot.ID())
	}
	return nil
}

func ballotBlockView(b *types.Ballot) []types.BlockID {
	combined := make([]types.BlockID, 0, len(b.AgainstDiff)+len(b.ForDiff)+len(b.NeutralDiff))
	combined = append(combined, b.ForDiff...)
	combined = append(combined, b.AgainstDiff...)
	combined = append(combined, b.NeutralDiff...)
	return combined
}

func (h *Handler) checkBallotDataAvailability(ctx context.Context, b *types.Ballot) error {
	ballots := []types.BallotID{b.BaseBallot}
	if b.RefBallot != types.EmptyBallotID {
		ballots = append(ballots, b.RefBallot)
	}
	if err := h.fetcher.GetBallots(ctx, ballots); err != nil {
		return fmt.Errorf("fetch ballots: %w", err)
	}

	if err := h.fetchReferencedATXs(ctx, b); err != nil {
		return fmt.Errorf("fetch referenced ATXs: %w", err)
	}

	if err := h.fetcher.GetBlocks(ctx, ballotBlockView(b)); err != nil {
		return fmt.Errorf("fetch blocks: %w", err)
	}

	return nil
}

func (h *Handler) fetchReferencedATXs(ctx context.Context, b *types.Ballot) error {
	atxs := []types.ATXID{b.AtxID}
	if b.EpochData != nil {
		atxs = append(atxs, b.EpochData.ActiveSet...)
	}
	if err := h.fetcher.GetAtxs(ctx, atxs); err != nil {
		return fmt.Errorf("proposal get ATXs: %w", err)
	}
	return nil
}

func (h *Handler) checkTransactions(ctx context.Context, p *types.Proposal) error {
	if len(p.TxIDs) == 0 {
		return nil
	}

	set := make(map[types.TransactionID]struct{}, len(p.TxIDs))
	for _, tx := range p.TxIDs {
		if _, exist := set[tx]; exist {
			return errDuplicateTX
		}
		set[tx] = struct{}{}
	}
	if err := h.fetcher.GetTxs(ctx, p.TxIDs); err != nil {
		return fmt.Errorf("proposal get TXs: %w", err)
	}
	return nil
}

func reportProposalMetrics(p *types.Proposal) {
	metrics.ProposalSize.WithLabelValues().Observe(float64(len(p.Bytes())))
	metrics.NumTxsInProposal.WithLabelValues().Observe(float64(len(p.TxIDs)))
}

func reportBallotMetrics(b *types.Ballot) {
	metrics.NumBlocksInException.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeAgainst}).Observe(float64(len(b.AgainstDiff)))
	metrics.NumBlocksInException.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeFor}).Observe(float64(len(b.ForDiff)))
	metrics.NumBlocksInException.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeNeutral}).Observe(float64(len(b.NeutralDiff)))
}
