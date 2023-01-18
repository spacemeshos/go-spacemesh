package proposals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

var (
	errMalformedData         = errors.New("malformed data")
	errInitialize            = errors.New("failed to initialize")
	errInvalidATXID          = errors.New("ballot has invalid ATXID")
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
	errKnownBallot           = errors.New("known ballot")
	errInvalidVote           = errors.New("invalid layer/height in the vote")
	errMaliciousBallot       = errors.New("malicious ballot")
)

// Handler processes Proposal from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger log.Log
	cfg    Config

	cdb       *datastore.CachedDB
	publisher pubsub.Publisher
	fetcher   system.Fetcher
	mesh      meshProvider
	validator eligibilityValidator
	decoder   ballotDecoder
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
func NewHandler(cdb *datastore.CachedDB, p pubsub.Publisher, f system.Fetcher, bc system.BeaconCollector, m meshProvider, decoder ballotDecoder, verifier vrfVerifier, nonceFetcher nonceFetcher, opts ...Opt) *Handler {
	b := &Handler{
		logger:    log.NewNop(),
		cfg:       defaultConfig(),
		cdb:       cdb,
		publisher: p,
		fetcher:   f,
		mesh:      m,
		decoder:   decoder,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.validator == nil {
		b.validator = NewEligibilityValidator(b.cfg.LayerSize, b.cfg.LayersPerEpoch, cdb, bc, m, b.logger, verifier, nonceFetcher)
	}
	return b
}

// HandleProposal is the gossip receiver for Proposal.
func (h *Handler) HandleProposal(ctx context.Context, peer p2p.Peer, msg []byte) pubsub.ValidationResult {
	err := h.handleProposalData(ctx, msg, peer)
	switch {
	case err == nil:
		return pubsub.ValidationAccept
	case errors.Is(err, errMalformedData):
		return pubsub.ValidationReject
	case errors.Is(err, errKnownProposal):
		return pubsub.ValidationIgnore
	default:
		h.logger.WithContext(ctx).With().Warning("failed to process proposal gossip", log.Err(err))
		return pubsub.ValidationIgnore
	}
}

// HandleSyncedBallot handles Ballot data from sync.
func (h *Handler) HandleSyncedBallot(ctx context.Context, data []byte) error {
	logger := h.logger.WithContext(ctx)

	var b types.Ballot
	t0 := time.Now()
	if err := codec.Decode(data, &b); err != nil {
		logger.With().Error("malformed ballot", log.Err(err))
		return errMalformedData
	}

	// set the ballot and smesher ID when received
	if err := b.Initialize(); err != nil {
		logger.With().Error("failed to initialize ballot", log.Err(err))
		return errInitialize
	}
	ballotDuration.WithLabelValues(decodeInit).Observe(float64(time.Since(t0)))

	t1 := time.Now()
	h.fetcher.AddPeersFromHash(b.ID().AsHash32(), collectHashes(b))
	ballotDuration.WithLabelValues(peerHashes).Observe(float64(time.Since(t1)))

	logger = logger.WithFields(b.ID(), b.Layer)
	if _, err := h.processBallot(ctx, logger, &b); err != nil {
		if errors.Is(err, errKnownBallot) {
			return nil
		}
		return err
	}
	return nil
}

// collectHashes gathers all hashes in a proposal or ballot.
func collectHashes(a any) []types.Hash32 {
	p, ok := a.(types.Proposal)
	if ok {
		hashes := collectHashes(p.Ballot)
		return append(hashes, types.TransactionIDsToHashes(p.TxIDs)...)
	}

	b, ok := a.(types.Ballot)
	if ok {
		hashes := types.BlockIDsToHashes(ballotBlockView(&b))
		if b.RefBallot != types.EmptyBallotID {
			hashes = append(hashes, b.RefBallot.AsHash32())
		}
		return append(hashes, b.Votes.Base.AsHash32())
	}
	log.Fatal("unexpected type")
	return nil
}

// HandleSyncedProposal handles Proposal data from sync.
func (h *Handler) HandleSyncedProposal(ctx context.Context, data []byte) error {
	err := h.handleProposalData(ctx, data, p2p.NoPeer)
	if errors.Is(err, errKnownProposal) {
		return nil
	}
	return err
}

func (h *Handler) handleProposalData(ctx context.Context, data []byte, peer p2p.Peer) error {
	logger := h.logger.WithContext(ctx)

	t0 := time.Now()
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
	proposalDuration.WithLabelValues(decodeInit).Observe(float64(time.Since(t0)))

	logger = logger.WithFields(p.ID(), p.Ballot.ID(), p.Layer)
	t1 := time.Now()
	if has, err := proposals.Has(h.cdb, p.ID()); err != nil {
		logger.With().Error("failed to look up proposal", log.Err(err))
		return fmt.Errorf("lookup proposal %v: %w", p.ID(), err)
	} else if has {
		logger.Debug("known proposal")
		return fmt.Errorf("%w proposal %s", errKnownProposal, p.ID())
	}
	proposalDuration.WithLabelValues(dbLookup).Observe(float64(time.Since(t1)))

	logger.With().Info("new proposal", log.Int("num_txs", len(p.TxIDs)))
	t2 := time.Now()
	h.fetcher.RegisterPeerHashes(peer, collectHashes(p))
	proposalDuration.WithLabelValues(peerHashes).Observe(float64(time.Since(t2)))

	t3 := time.Now()
	proof, err := h.processBallot(ctx, logger, &p.Ballot)
	if err != nil && !errors.Is(err, errKnownBallot) && !errors.Is(err, errMaliciousBallot) {
		logger.With().Warning("failed to process ballot", log.Err(err))
		return err
	}
	proposalDuration.WithLabelValues(ballot).Observe(float64(time.Since(t3)))

	// FIXME: how to handle proposals from malicious identity?
	t4 := time.Now()
	if err := h.checkTransactions(ctx, &p); err != nil {
		logger.With().Warning("failed to fetch proposal TXs", log.Err(err))
		return err
	}
	proposalDuration.WithLabelValues(fetchTXs).Observe(float64(time.Since(t4)))

	logger.With().Debug("proposal is syntactically valid")
	t5 := time.Now()
	if err := proposals.Add(h.cdb, &p); err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			return fmt.Errorf("%w proposal %s", errKnownProposal, p.ID())
		}
		logger.With().Error("failed to save proposal", log.Err(err))
		return fmt.Errorf("save proposal: %w", err)
	}
	proposalDuration.WithLabelValues(dbSave).Observe(float64(time.Since(t5)))
	logger.With().Info("added proposal to database")

	t6 := time.Now()
	if err := h.mesh.AddTXsFromProposal(ctx, p.Layer, p.ID(), p.TxIDs); err != nil {
		return fmt.Errorf("proposal add TXs: %w", err)
	}
	proposalDuration.WithLabelValues(linkTxs).Observe(float64(time.Since(t6)))

	reportProposalMetrics(&p)

	// broadcast malfeasance proof last as the verification of the proof will take place
	// in the same goroutine
	if proof != nil {
		gossip := types.MalfeasanceGossip{
			MalfeasanceProof: *proof,
		}
		encodedProof, err := codec.Encode(&gossip)
		if err != nil {
			h.logger.Fatal("failed to encode MalfeasanceGossip", log.Err(err))
		}
		if err = h.publisher.Publish(ctx, pubsub.MalfeasanceProof, encodedProof); err != nil {
			logger.With().Error("failed to broadcast malfeasance proof", log.Err(err))
			return fmt.Errorf("broadcast ballot malfeasance proof: %w", err)
		}
		return errMaliciousBallot
	}
	return nil
}

func (h *Handler) processBallot(ctx context.Context, logger log.Log, b *types.Ballot) (*types.MalfeasanceProof, error) {
	t0 := time.Now()
	if has, err := ballots.Has(h.cdb, b.ID()); err != nil {
		logger.With().Error("failed to look up ballot", log.Err(err))
		return nil, fmt.Errorf("lookup ballot %v: %w", b.ID(), err)
	} else if has {
		logger.Debug("known ballot", b.ID())
		return nil, fmt.Errorf("%w: ballot %s", errKnownBallot, b.ID())
	}
	ballotDuration.WithLabelValues(dbLookup).Observe(float64(time.Since(t0)))

	logger.With().Info("new ballot", log.Inline(b))

	decoded, err := h.checkBallotSyntacticValidity(ctx, logger, b)
	if err != nil {
		return nil, err
	}

	t1 := time.Now()
	proof, err := h.mesh.AddBallot(ctx, b)
	if err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			return nil, fmt.Errorf("%w: ballot %s", errKnownBallot, b.ID())
		}
		return nil, fmt.Errorf("save ballot: %w", err)
	}
	ballotDuration.WithLabelValues(dbSave).Observe(float64(time.Since(t1)))
	if err := h.decoder.StoreBallot(decoded); err != nil {
		return nil, fmt.Errorf("store decoded ballot %s: %w", decoded.ID(), err)
	}
	reportVotesMetrics(b)
	return proof, nil
}

func (h *Handler) checkBallotSyntacticValidity(ctx context.Context, logger log.Log, b *types.Ballot) (*tortoise.DecodedBallot, error) {
	logger.With().Debug("checking proposal syntactic validity")

	t0 := time.Now()
	if err := h.checkBallotDataIntegrity(b); err != nil {
		logger.With().Warning("ballot integrity check failed", log.Err(err))
		return nil, err
	}
	ballotDuration.WithLabelValues(dataCheck).Observe(float64(time.Since(t0)))

	t1 := time.Now()
	if err := h.checkBallotDataAvailability(ctx, b); err != nil {
		logger.With().Warning("ballot data availability check failed", log.Err(err))
		return nil, err
	}
	ballotDuration.WithLabelValues(fetchRef).Observe(float64(time.Since(t1)))

	t2 := time.Now()
	// ballot can be decoded only if all dependencies (blocks, ballots, atxs) were downloaded
	// and added to the tortoise.
	decoded, err := h.decoder.DecodeBallot(b)
	if err != nil {
		return nil, fmt.Errorf("decode ballot %s: %w", b.ID(), err)
	}
	ballotDuration.WithLabelValues(decode).Observe(float64(time.Since(t2)))

	t3 := time.Now()
	// note that computed opinion has to match signed opinion, otherwise it is unknown
	// if attached votes struct was modified
	//
	// TODO this check can work only on the list with decoded votes, otherwise
	// otherwise it validates only diff, which is easy to bypass
	if err := h.checkVotesConsistency(ctx, b); err != nil {
		logger.With().Warning("ballot votes consistency check failed", log.Err(err))
		return nil, err
	}
	ballotDuration.WithLabelValues(votes).Observe(float64(time.Since(t3)))

	t4 := time.Now()
	if eligible, err := h.validator.CheckEligibility(ctx, b); err != nil || !eligible {
		h.logger.WithContext(ctx).With().Warning("ballot eligibility check failed", log.Err(err))
		return nil, errNotEligible
	}
	ballotDuration.WithLabelValues(eligible).Observe(float64(time.Since(t4)))

	logger.With().Debug("ballot is syntactically valid")
	return decoded, nil
}

func (h *Handler) checkBallotDataIntegrity(b *types.Ballot) error {
	if b.AtxID == *types.EmptyATXID || b.AtxID == h.cfg.GoldenATXID {
		return errInvalidATXID
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

func (h *Handler) checkVotesConsistency(ctx context.Context, b *types.Ballot) error {
	exceptions := map[types.BlockID]struct{}{}
	layers := make(map[types.LayerID]types.BlockID)
	// a ballot should not vote for multiple blocks in the same layer within hdist,
	// since hare only output a single block each layer and miner should vote according
	// to the hare output within hdist of the current layer when producing a ballot.
	for _, vote := range b.Votes.Support {
		exceptions[vote.ID] = struct{}{}
		block, err := blocks.Get(h.cdb, vote.ID)
		if err != nil {
			h.logger.WithContext(ctx).With().Error("failed to get block layer", log.Err(err))
			return fmt.Errorf("check exception get block layer: %w", err)
		}
		if block.ToVote() != vote {
			return fmt.Errorf("%w: encoded vote %+v doesn't match actual %+v", errInvalidVote, vote, block.ToVote())
		}
		if voted, ok := layers[vote.LayerID]; ok {
			// already voted for a block in this layer
			if voted != vote.ID && vote.LayerID.Add(h.cfg.Hdist).After(b.Layer) {
				h.logger.WithContext(ctx).With().Warning("ballot doubly voted within hdist, set smesher malicious",
					b.ID(),
					b.Layer,
					log.Stringer("smesher", b.SmesherID()),
					log.Stringer("voted_bid", voted),
					log.Stringer("voted_bid", vote.ID),
					log.Uint32("hdist", h.cfg.Hdist))
				return errDoubleVoting
			}
		} else {
			layers[vote.LayerID] = vote.ID
		}
	}
	// a ballot should not vote support and against on the same block.
	for _, vote := range b.Votes.Against {
		if _, exist := exceptions[vote.ID]; exist {
			h.logger.WithContext(ctx).With().Warning("conflicting votes on block", vote.ID, b.ID(), b.Layer)
			return fmt.Errorf("%w: block %s is referenced multiple times in exceptions of ballot %s at layer %v",
				errConflictingExceptions, vote.ID, b.ID(), b.Layer)
		}
		block, err := blocks.Get(h.cdb, vote.ID)
		if err != nil {
			h.logger.WithContext(ctx).With().Error("failed to get block layer", log.Err(err))
			return fmt.Errorf("check exception get block layer: %w", err)
		}
		if block.ToVote() != vote {
			return fmt.Errorf("%w encoded vote %+v doesn't match actual %+v",
				errInvalidVote, vote, block.ToVote())
		}
		layers[vote.LayerID] = vote.ID
	}
	if len(exceptions) > h.cfg.MaxExceptions {
		h.logger.WithContext(ctx).With().Warning("exceptions exceed limits",
			b.ID(),
			b.Layer,
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
				b.Layer,
				log.Stringer("conflict_layer", lid))
			return errConflictingExceptions
		}
	}
	return nil
}

func ballotBlockView(b *types.Ballot) []types.BlockID {
	combined := make([]types.BlockID, 0, len(b.Votes.Support)+len(b.Votes.Against))
	for _, vote := range b.Votes.Support {
		combined = append(combined, vote.ID)
	}
	for _, vote := range b.Votes.Against {
		combined = append(combined, vote.ID)
	}
	return combined
}

func (h *Handler) checkBallotDataAvailability(ctx context.Context, b *types.Ballot) error {
	blts := []types.BallotID{}
	if b.Votes.Base != types.EmptyBallotID {
		blts = append(blts, b.Votes.Base)
	}
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
		if err := h.fetcher.GetBlocks(ctx, bids); err != nil {
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
	proposalSize.WithLabelValues().Observe(float64(len(p.Bytes())))
	numTxsInProposal.WithLabelValues().Observe(float64(len(p.TxIDs)))
}

func reportVotesMetrics(b *types.Ballot) {
	numBlocksInException.With(prometheus.Labels{diffTypeLabel: diffTypeAgainst}).Observe(float64(len(b.Votes.Against)))
	numBlocksInException.With(prometheus.Labels{diffTypeLabel: diffTypeFor}).Observe(float64(len(b.Votes.Support)))
	numBlocksInException.With(prometheus.Labels{diffTypeLabel: diffTypeNeutral}).Observe(float64(len(b.Votes.Abstain)))
}
