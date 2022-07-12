package proposals

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/metrics"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
)

// NewProposalProtocol is the protocol indicator for gossip Proposals.
const NewProposalProtocol = "newProposal/1"

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
	errDoubleVoting          = errors.New("ballot doubly-voted in same layer")
	errConflictingExceptions = errors.New("conflicting exceptions")
	errExceptionsOverflow    = errors.New("too many exceptions")
	errDuplicateTX           = errors.New("duplicate TxID in proposal")
	errDuplicateATX          = errors.New("duplicate ATXID in active set")
	errKnownProposal         = errors.New("known proposal")
)

// Handler processes Proposal from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger log.Log
	cfg    Config

	cdb       *datastore.CachedDB
	fetcher   system.Fetcher
	mesh      meshProvider
	validator eligibilityValidator
}

// Config defines configuration for the handler.
type Config struct {
	LayerSize      uint32
	LayersPerEpoch uint32
	GoldenATXID    types.ATXID
	MaxExceptions  int
	Hdist          uint32
}

// defaultConfig for BlockHandler.
func defaultConfig() Config {
	return Config{
		MaxExceptions: 1000,
	}
}

// Opt for configuring Handler.
type Opt func(h *Handler)

// withValidator defines eligibility Validator.
func withValidator(v eligibilityValidator) Opt {
	return func(h *Handler) {
		h.validator = v
	}
}

// WithLogger defines logger for Handler.
func WithLogger(logger log.Log) Opt {
	return func(h *Handler) {
		h.logger = logger
	}
}

// WithConfig defines protocol parameters.
func WithConfig(cfg Config) Opt {
	return func(h *Handler) {
		h.cfg = cfg
	}
}

// NewHandler creates new Handler.
func NewHandler(cdb *datastore.CachedDB, f system.Fetcher, bc system.BeaconCollector, m meshProvider, opts ...Opt) *Handler {
	b := &Handler{
		logger:  log.NewNop(),
		cfg:     defaultConfig(),
		cdb:     cdb,
		fetcher: f,
		mesh:    m,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.validator == nil {
		b.validator = NewEligibilityValidator(b.cfg.LayerSize, b.cfg.LayersPerEpoch, cdb, bc, m, b.logger)
	}
	return b
}

// HandleProposal is the gossip receiver for Proposal.
func (h *Handler) HandleProposal(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	newCtx := log.WithNewRequestID(ctx)
	if err := h.handleProposalData(newCtx, msg, peer); errors.Is(err, errKnownProposal) {
		return pubsub.ValidationIgnore
	} else if err != nil {
		h.logger.WithContext(newCtx).With().Error("failed to process proposal gossip", log.Err(err))
		return pubsub.ValidationIgnore
	}
	return pubsub.ValidationAccept
}

// HandleBallotData handles Ballot data from sync.
func (h *Handler) HandleBallotData(ctx context.Context, data []byte) error {
	newCtx := log.WithNewRequestID(ctx)
	logger := h.logger.WithContext(newCtx)

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

	bidHash := b.ID().AsHash32()
	h.fetcher.AddPeersFromHash(bidHash, collectHashes(&b))

	logger = logger.WithFields(b.ID(), b.LayerIndex)
	if err := h.processBallot(ctx, &b, logger); err != nil {
		return err
	}
	return nil
}

// collectHashes gathers all hashes in the ballot.
func collectHashes(b *types.Ballot) []types.Hash32 {
	hashes := types.BlockIDsToHashes(ballotBlockView(b))
	if b.RefBallot != types.EmptyBallotID {
		hashes = append(hashes, b.RefBallot.AsHash32())
	}
	hashes = append(hashes, b.Votes.Base.AsHash32())
	return hashes
}

// HandleProposalData handles Proposal data from sync.
func (h *Handler) HandleProposalData(ctx context.Context, data []byte) error {
	err := h.handleProposalData(ctx, data, p2p.NoPeer)
	if errors.Is(err, errKnownProposal) {
		return nil
	}
	return err
}

func (h *Handler) handleProposalData(ctx context.Context, data []byte, peer p2p.Peer) error {
	logger := h.logger.WithContext(ctx)

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

	if has, err := proposals.Has(h.cdb, p.ID()); err != nil {
		logger.With().Error("failed to look up proposal", p.ID(), log.Err(err))
		return fmt.Errorf("lookup proposal %v: %w", p.ID(), err)
	} else if has {
		logger.Info("known proposal")
		return fmt.Errorf("%w proposal %s", errKnownProposal, p.ID())
	}

	logger.With().Info("new proposal", p.ID())

	h.fetcher.RegisterPeerHashes(peer, collectHashes(&p.Ballot))

	if err := h.processBallot(ctx, &p.Ballot, logger); err != nil {
		logger.With().Warning("failed to process ballot", log.Err(err))
		return err
	}

	if err := h.checkTransactions(ctx, &p); err != nil {
		logger.With().Warning("failed to fetch proposal TXs", log.Err(err))
		return err
	}

	logger.With().Debug("proposal is syntactically valid")

	if err := proposals.Add(h.cdb, &p); err != nil {
		logger.With().Error("failed to save proposal", log.Err(err))
		return fmt.Errorf("save proposal: %w", err)
	}

	logger.With().Info("added proposal to database", p.ID())

	if err := h.mesh.AddTXsFromProposal(ctx, p.LayerIndex, p.ID(), p.TxIDs); err != nil {
		return fmt.Errorf("proposal add TXs: %w", err)
	}

	reportProposalMetrics(&p)
	return nil
}

func (h *Handler) processBallot(ctx context.Context, b *types.Ballot, logger log.Log) error {
	if has, err := ballots.Has(h.cdb, b.ID()); err != nil {
		h.logger.WithContext(ctx).With().Error("failed to look up ballot", b.ID(), log.Err(err))
		return fmt.Errorf("lookup ballot %v: %w", b.ID(), err)
	} else if has {
		logger.Debug("known ballot", b.ID())
		return nil
	}

	logger.With().Info("new ballot", log.Inline(b))

	if err := h.checkBallotSyntacticValidity(ctx, b); err != nil {
		logger.With().Error("ballot syntactically invalid", log.Err(err))
		return fmt.Errorf("syntactic-check ballot: %w", err)
	}

	if err := ballots.Add(h.cdb, b); err != nil {
		return fmt.Errorf("save ballot: %w", err)
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

	if err := h.checkVotesConsistency(ctx, b); err != nil {
		h.logger.WithContext(ctx).With().Warning("ballot votes consistency check failed", log.Err(err))
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

	if b.Votes.Base == types.EmptyBallotID {
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
	} else if b.EpochData != nil {
		return errUnexpectedEpochData
	}
	return nil
}

func (h *Handler) setBallotMalicious(ctx context.Context, b *types.Ballot) error {
	if err := identities.SetMalicious(h.cdb, b.SmesherID().Bytes()); err != nil {
		h.logger.WithContext(ctx).With().Error("failed to set smesher malicious",
			b.ID(),
			b.LayerIndex,
			log.Stringer("smesher", b.SmesherID()),
			log.Err(err))
		return fmt.Errorf("set smesher malcious: %w", err)
	}
	b.SetMalicious()
	return nil
}

func (h *Handler) checkVotesConsistency(ctx context.Context, b *types.Ballot) error {
	exceptions := map[types.BlockID]struct{}{}
	cutoff := types.LayerID{}
	if b.LayerIndex.After(types.NewLayerID(h.cfg.Hdist)) {
		cutoff = b.LayerIndex.Sub(h.cfg.Hdist)
	}
	layers := make(map[types.LayerID]types.BlockID)
	// a ballot should not vote for multiple blocks in the same layer within hdist,
	// since hare only output a single block each layer and miner should vote according
	// to the hare output within hdist of the current layer when producing a ballot.
	for _, bid := range b.Votes.Support {
		exceptions[bid] = struct{}{}
		lid, err := blocks.GetLayer(h.cdb, bid)
		if err != nil {
			h.logger.WithContext(ctx).With().Error("failed to get block layer", log.Err(err))
			return fmt.Errorf("check exception get block layer: %w", err)
		}
		if voted, ok := layers[lid]; ok {
			// already voted for a block in this layer
			if voted != bid && !lid.Before(cutoff) {
				h.logger.WithContext(ctx).With().Warning("ballot doubly voted within hdist, set smesher malicious",
					b.ID(),
					b.LayerIndex,
					log.Stringer("smesher", b.SmesherID()),
					log.Stringer("voted_bid", voted),
					log.Stringer("voted_bid", bid),
					log.Uint32("hdist", h.cfg.Hdist))
				if err = h.setBallotMalicious(ctx, b); err != nil {
					return err
				}
				return errDoubleVoting
			}
		} else {
			layers[lid] = bid
		}
	}
	// a ballot should not vote support and against on the same block.
	for _, bid := range b.Votes.Against {
		if _, exist := exceptions[bid]; exist {
			h.logger.WithContext(ctx).With().Warning("conflicting votes on block", bid, b.ID(), b.LayerIndex)
			if err := h.setBallotMalicious(ctx, b); err != nil {
				return err
			}
			return fmt.Errorf("%w: block %s is referenced multiple times in exceptions of ballot %s at layer %v",
				errConflictingExceptions, bid, b.ID(), b.LayerIndex)
		}
		lid, err := blocks.GetLayer(h.cdb, bid)
		if err != nil {
			h.logger.WithContext(ctx).With().Error("failed to get block layer", log.Err(err))
			return fmt.Errorf("check exception get block layer: %w", err)
		}
		layers[lid] = bid
	}
	if len(exceptions) > h.cfg.MaxExceptions {
		h.logger.WithContext(ctx).With().Warning("exceptions exceed limits",
			b.ID(),
			b.LayerIndex,
			log.Int("len", len(exceptions)),
			log.Int("max_allowed", h.cfg.MaxExceptions))
		return fmt.Errorf("%w: %d exceptions with max allowed %d in ballot %s",
			errExceptionsOverflow, len(exceptions), h.cfg.MaxExceptions, b.ID())
	}
	// a ballot should not abstain on a layer that it voted for/against on block in that layer.
	for _, lid := range b.Votes.Abstain {
		if _, ok := layers[lid]; ok {
			h.logger.WithContext(ctx).With().Warning("conflicting votes on layer",
				b.ID(),
				b.LayerIndex,
				log.Stringer("conflict_layer", lid))
			if err := h.setBallotMalicious(ctx, b); err != nil {
				return err
			}
			return errConflictingExceptions
		}
	}
	return nil
}

func ballotBlockView(b *types.Ballot) []types.BlockID {
	combined := make([]types.BlockID, 0, len(b.Votes.Support)+len(b.Votes.Against))
	combined = append(combined, b.Votes.Support...)
	combined = append(combined, b.Votes.Against...)
	return combined
}

func (h *Handler) checkBallotDataAvailability(ctx context.Context, b *types.Ballot) error {
	blts := []types.BallotID{b.Votes.Base}
	if b.RefBallot != types.EmptyBallotID {
		blts = append(blts, b.RefBallot)
	}
	if err := h.fetcher.GetBallots(ctx, blts); err != nil {
		return fmt.Errorf("fetch ballots: %w", err)
	}

	if err := h.fetchReferencedATXs(ctx, b); err != nil {
		return fmt.Errorf("fetch referenced ATXs: %w", err)
	}

	bids := ballotBlockView(b)
	if len(bids) > 0 {
		if err := h.fetcher.GetBlocks(ctx, ballotBlockView(b)); err != nil {
			return fmt.Errorf("fetch blocks: %w", err)
		}
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
	if err := h.fetcher.GetProposalTxs(ctx, p.TxIDs); err != nil {
		return fmt.Errorf("proposal get TXs: %w", err)
	}
	return nil
}

func reportProposalMetrics(p *types.Proposal) {
	metrics.ProposalSize.WithLabelValues().Observe(float64(len(p.Bytes())))
	metrics.NumTxsInProposal.WithLabelValues().Observe(float64(len(p.TxIDs)))
}

func reportBallotMetrics(b *types.Ballot) {
	metrics.NumBlocksInException.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeAgainst}).Observe(float64(len(b.Votes.Against)))
	metrics.NumBlocksInException.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeFor}).Observe(float64(len(b.Votes.Support)))
	metrics.NumBlocksInException.With(prometheus.Labels{metrics.DiffTypeLabel: metrics.DiffTypeNeutral}).Observe(float64(len(b.Votes.Abstain)))
}
