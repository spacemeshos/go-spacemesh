package proposals

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

var (
	errMalformedData         = fmt.Errorf("%w: malformed data", pubsub.ErrValidationReject)
	errWrongHash             = fmt.Errorf("%w: incorrect hash", pubsub.ErrValidationReject)
	errInitialize            = fmt.Errorf("%w: failed to initialize", pubsub.ErrValidationReject)
	errInvalidATXID          = fmt.Errorf("%w: ballot has invalid ATXID", pubsub.ErrValidationReject)
	errDoubleVoting          = fmt.Errorf("%w: ballot doubly-voted in same layer", pubsub.ErrValidationReject)
	errConflictingExceptions = fmt.Errorf("%w: conflicting exceptions", pubsub.ErrValidationReject)
	errExceptionsOverflow    = fmt.Errorf("%w: too many exceptions", pubsub.ErrValidationReject)
	errDuplicateTX           = fmt.Errorf("%w: duplicate TxID in proposal", pubsub.ErrValidationReject)
	errKnownProposal         = errors.New("known proposal")
	errKnownBallot           = errors.New("known ballot")
	errMaliciousBallot       = errors.New("malicious ballot")
)

// Handler processes Proposal from gossip and, if deems it valid, propagates it to peers.
type Handler struct {
	logger *zap.Logger
	cfg    Config

	db                sql.StateDatabase
	atxsdata          *atxsdata.Data
	activeSets        *lru.Cache[types.Hash32, uint64]
	edVerifier        *signing.EdVerifier
	publisher         pubsub.Publisher
	fetcher           system.Fetcher
	mesh              meshProvider
	validator         eligibilityValidator
	tortoise          tortoiseProvider
	weightCalcLock    sync.Mutex
	pendingWeightCalc map[types.Hash32][]chan uint64
	clock             layerClock

	proposals proposalsConsumer
}

// Config defines configuration for the handler.
type Config struct {
	LayerSize              uint32
	LayersPerEpoch         uint32
	GoldenATXID            types.ATXID
	MaxExceptions          int
	Hdist                  uint32
	MinimalActiveSetWeight []types.EpochMinimalActiveWeight
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
func WithLogger(logger *zap.Logger) Opt {
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
func NewHandler(
	db sql.StateDatabase,
	atxsdata *atxsdata.Data,
	proposals proposalsConsumer,
	edVerifier *signing.EdVerifier,
	p pubsub.Publisher,
	f system.Fetcher,
	bc system.BeaconCollector,
	m meshProvider,
	tortoise tortoiseProvider,
	verifier vrfVerifier,
	clock layerClock,
	opts ...Opt,
) *Handler {
	activeSets, err := lru.New[types.Hash32, uint64](10_000)
	if err != nil {
		panic(err)
	}
	b := &Handler{
		logger:            zap.NewNop(),
		cfg:               defaultConfig(),
		db:                db,
		atxsdata:          atxsdata,
		proposals:         proposals,
		activeSets:        activeSets,
		edVerifier:        edVerifier,
		publisher:         p,
		fetcher:           f,
		mesh:              m,
		tortoise:          tortoise,
		pendingWeightCalc: make(map[types.Hash32][]chan uint64),
		clock:             clock,
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.validator == nil {
		b.validator = NewEligibilityValidator(
			b.cfg.LayerSize,
			b.cfg.LayersPerEpoch,
			b.cfg.MinimalActiveSetWeight,
			clock,
			tortoise,
			atxsdata,
			bc,
			b.logger,
			verifier,
		)
	}
	return b
}

// HandleSyncedBallot handles Ballot data from sync.
func (h *Handler) HandleSyncedBallot(ctx context.Context, expHash types.Hash32, peer p2p.Peer, data []byte) error {
	var b types.Ballot
	t0 := time.Now()
	if err := codec.Decode(data, &b); err != nil {
		malformed.Inc()
		return errMalformedData
	}
	if b.Layer <= types.GetEffectiveGenesis() {
		return fmt.Errorf("%w: ballot before effective genesis: layer %v", pubsub.ErrValidationReject, b.Layer)
	}

	if !h.edVerifier.Verify(signing.BALLOT, b.SmesherID, b.SignedBytes(), b.Signature) {
		return fmt.Errorf(
			"%w: failed to verify ballot %s signature",
			pubsub.ErrValidationReject,
			b.ID().String(),
		)
	}

	// set the ballot and smesher ID when received
	if err := b.Initialize(); err != nil {
		failedInit.Inc()
		return errInitialize
	}

	if b.ID().AsHash32() != expHash {
		return fmt.Errorf(
			"%w: ballot want %s, got %s",
			pubsub.ErrValidationReject,
			expHash.ShortString(),
			b.ID().String(),
		)
	}

	if b.AtxID == types.EmptyATXID || b.AtxID == h.cfg.GoldenATXID {
		return errInvalidATXID
	}
	ballotDuration.WithLabelValues(decodeInit).Observe(float64(time.Since(t0)))

	t1 := time.Now()
	h.fetcher.RegisterPeerHashes(peer, collectHashes(b))
	ballotDuration.WithLabelValues(peerHashes).Observe(float64(time.Since(t1)))

	if _, err := h.processBallot(ctx, &b); err != nil {
		if errors.Is(err, errKnownBallot) {
			return nil
		}
		return err
	}
	return nil
}

func (h *Handler) HandleActiveSet(ctx context.Context, id types.Hash32, peer p2p.Peer, data []byte) error {
	var set types.EpochActiveSet
	if err := codec.Decode(data, &set); err != nil {
		return fmt.Errorf("%w: malformed active set %s", pubsub.ErrValidationReject, err.Error())
	}
	h.fetcher.RegisterPeerHashes(peer, types.ATXIDsToHashes(set.Set))
	return h.handleSet(ctx, id, set)
}

func (h *Handler) handleSet(ctx context.Context, id types.Hash32, set types.EpochActiveSet) error {
	if !slices.IsSortedFunc(set.Set, func(left, right types.ATXID) int {
		return bytes.Compare(left[:], right[:])
	}) {
		return fmt.Errorf("%w: active set is not sorted", pubsub.ErrValidationReject)
	}
	if id != types.ATXIDList(set.Set).Hash() {
		return fmt.Errorf("%w: response for wrong hash %s", pubsub.ErrValidationReject, id.String())
	}
	// active set is invalid unless all activations that it references are from the correct epoch
	_, used := h.atxsdata.WeightForSet(set.Epoch, set.Set)
	var atxids []types.ATXID
	for i := range set.Set {
		if !used[i] {
			atxids = append(atxids, set.Set[i])
		}
	}
	if err := h.fetcher.GetAtxs(ctx, atxids); err != nil {
		return err
	}
	err := activesets.Add(h.db, id, &set)
	if err != nil && !errors.Is(err, sql.ErrObjectExists) {
		return err
	}
	return nil
}

// collectHashes gathers all hashes in a proposal or ballot.
func collectHashes(a any) []types.Hash32 {
	switch typed := a.(type) {
	case types.Proposal:
		return append(collectHashes(typed.Ballot), types.TransactionIDsToHashes(typed.TxIDs)...)
	case types.Ballot:
		hashes := []types.Hash32{typed.Votes.Base.AsHash32()}
		if typed.RefBallot != types.EmptyBallotID {
			hashes = append(hashes, typed.RefBallot.AsHash32())
		} else if typed.EpochData != nil {
			hashes = append(hashes, typed.EpochData.ActiveSetHash)
		}
		for _, header := range typed.Votes.Support {
			hashes = append(hashes, header.ID.AsHash32())
		}
		return hashes
	}
	panic("unexpected type")
}

// HandleSyncedProposal handles Proposal data from sync.
func (h *Handler) HandleSyncedProposal(ctx context.Context, expHash types.Hash32, peer p2p.Peer, data []byte) error {
	err := h.handleProposal(ctx, expHash, peer, data)
	if errors.Is(err, errKnownProposal) {
		return nil
	}
	return err
}

// HandleProposal is the gossip receiver for Proposal.
func (h *Handler) HandleProposal(ctx context.Context, peer p2p.Peer, data []byte) error {
	err := h.handleProposal(ctx, types.Hash32{}, peer, data)
	if err != nil {
		h.logger.Debug("failed to process proposal gossip", log.ZContext(ctx), zap.Error(err))
	}
	return err
}

// HandleProposal is the gossip receiver for Proposal.
func (h *Handler) handleProposal(ctx context.Context, expHash types.Hash32, peer p2p.Peer, data []byte) error {
	t0 := time.Now()
	var p types.Proposal
	if err := codec.Decode(data, &p); err != nil {
		malformed.Inc()
		return errMalformedData
	}
	if p.Layer <= types.GetEffectiveGenesis() {
		preGenesis.Inc()
		return fmt.Errorf("proposal before effective genesis: %d/%s", p.Layer, p.ID().String())
	} else if p.Layer <= h.mesh.ProcessedLayer() {
		tooLate.Inc()
		return fmt.Errorf("proposal too late: %d/%s", p.Layer, p.ID().String())
	} else if p.Layer >= h.clock.CurrentLayer()+1 {
		tooFuture.Inc()
		return fmt.Errorf("proposal from future: %d/%s", p.Layer, p.ID().String())
	}

	if !h.edVerifier.Verify(signing.PROPOSAL, p.SmesherID, p.SignedBytes(), p.Signature) {
		badSigProposal.Inc()
		return errors.New("failed to verify proposal signature")
	}
	if !h.edVerifier.Verify(signing.BALLOT, p.Ballot.SmesherID, p.Ballot.SignedBytes(), p.Ballot.Signature) {
		badSigBallot.Inc()
		return errors.New("failed to verify ballot signature")
	}

	// set the proposal ID when received
	if err := p.Initialize(); err != nil {
		failedInit.Inc()
		return errInitialize
	}
	if expHash != (types.Hash32{}) && p.ID().AsHash32() != expHash {
		return fmt.Errorf(
			"%w: proposal want %s, got %s",
			errWrongHash,
			expHash.ShortString(),
			p.ID().AsHash32().ShortString(),
		)
	}

	if p.AtxID == types.EmptyATXID || p.AtxID == h.cfg.GoldenATXID {
		badData.Inc()
		return errInvalidATXID
	}
	proposalDuration.WithLabelValues(decodeInit).Observe(float64(time.Since(t0)))

	logger := h.logger.With(
		log.ZContext(ctx),
		zap.Stringer("proposal", p.ID()),
		zap.Stringer("ballot", p.Ballot.ID()),
		zap.Uint32("layer", p.Layer.Uint32()),
	)
	if h.proposals.IsKnown(p.Layer, p.ID()) {
		known.Inc()
		return fmt.Errorf("%w proposal %s", errKnownProposal, p.ID())
	}
	logger.Debug("new proposal", log.ZShortStringer("exp hash", expHash), zap.Int("num_txs", len(p.TxIDs)))

	t2 := time.Now()
	h.fetcher.RegisterPeerHashes(peer, collectHashes(p))
	proposalDuration.WithLabelValues(peerHashes).Observe(float64(time.Since(t2)))

	t3 := time.Now()
	proof, err := h.processBallot(ctx, &p.Ballot)
	if err != nil && !errors.Is(err, errKnownBallot) && !errors.Is(err, errMaliciousBallot) {
		return err
	}
	proposalDuration.WithLabelValues(ballot).Observe(float64(time.Since(t3)))

	// FIXME: how to handle proposals from malicious identity?
	t4 := time.Now()
	if err := h.checkTransactions(ctx, &p); err != nil {
		unavailRef.Inc()
		return err
	}
	proposalDuration.WithLabelValues(fetchTXs).Observe(float64(time.Since(t4)))
	if err := h.setProposalBeacon(&p); err != nil {
		return fmt.Errorf("setting proposal beacon: %w", err)
	}
	logger.Debug("proposal is syntactically valid")

	err = h.proposals.OnProposal(&p)
	switch {
	case errors.Is(err, store.ErrProposalExists):
		known.Inc()
		return fmt.Errorf("%w %s", errKnownProposal, p.ID())
	case err != nil:
		return fmt.Errorf("saving proposal: %w", err)
	}
	logger.Debug("stored proposal")

	t6 := time.Now()
	if err = h.mesh.AddTXsFromProposal(ctx, p.Layer, p.ID(), p.TxIDs); err != nil {
		logger.Error("failed to link txs to proposal", zap.Error(err))
		return fmt.Errorf("proposal add TXs: %w", err)
	}
	proposalDuration.WithLabelValues(linkTxs).Observe(float64(time.Since(t6)))

	reportProposalMetrics(&p)

	// broadcast malfeasance proof last as the verification of the proof will take place
	// in the same goroutine
	if proof != nil {
		gossip := wire.MalfeasanceGossip{
			MalfeasanceProof: *proof,
		}
		encodedProof, err := codec.Encode(&gossip)
		if err != nil {
			h.logger.Fatal("failed to encode MalfeasanceGossip", zap.Error(err))
		}
		if err = h.publisher.Publish(ctx, pubsub.MalfeasanceProof, encodedProof); err != nil {
			failedPublish.Inc()
			logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
			return fmt.Errorf("broadcast ballot malfeasance proof: %w", err)
		}
		return errMaliciousBallot
	}
	metrics.ReportMessageLatency(
		pubsub.ProposalProtocol,
		pubsub.ProposalProtocol,
		time.Since(h.clock.LayerToTime(p.Layer)),
	)
	return nil
}

func (h *Handler) setProposalBeacon(p *types.Proposal) error {
	if p.EpochData != nil {
		p.SetBeacon(p.EpochData.Beacon)
		return nil
	}
	if p.RefBallot == types.EmptyBallotID {
		return errors.New("empty refballot")
	}
	refBallot, err := ballots.Get(h.db, p.RefBallot)
	if err != nil {
		return fmt.Errorf("cannot find refballot '%s' in DB: %w", p.RefBallot.String(), err)
	}
	if refBallot.EpochData == nil {
		return fmt.Errorf("refballot '%s' with empty epoch data", p.RefBallot.String())
	}
	p.SetBeacon(refBallot.EpochData.Beacon)
	return nil
}

func (h *Handler) processBallot(ctx context.Context, b *types.Ballot) (*wire.MalfeasanceProof, error) {
	if data := h.tortoise.GetBallot(b.ID()); data != nil {
		known.Inc()
		return nil, fmt.Errorf("%w: ballot %s", errKnownBallot, b.ID())
	}

	h.logger.Debug("new ballot", log.ZContext(ctx), zap.Inline(b))

	decoded, err := h.checkBallotSyntacticValidity(ctx, b)
	if err != nil {
		return nil, err
	}
	b.ActiveSet = nil // the active set is not needed anymore
	h.logger.Debug("ballot is syntactically valid", log.ZContext(ctx), zap.Stringer("id", b.ID()))

	t1 := time.Now()
	proof, err := h.mesh.AddBallot(ctx, b)
	if err != nil {
		if errors.Is(err, sql.ErrObjectExists) {
			known.Inc()
			return nil, fmt.Errorf("%w: ballot %s", errKnownBallot, b.ID())
		}
		return nil, fmt.Errorf("save ballot: %w", err)
	}
	ballotDuration.WithLabelValues(dbSave).Observe(float64(time.Since(t1)))
	if err := h.tortoise.StoreBallot(decoded); err != nil {
		if errors.Is(err, tortoise.ErrBallotExists) {
			return nil, fmt.Errorf("%w: %s", errKnownBallot, b.ID())
		}
		return nil, fmt.Errorf("store decoded ballot %s: %w", decoded.ID, err)
	}
	reportVotesMetrics(b)
	return proof, nil
}

func (h *Handler) checkBallotSyntacticValidity(ctx context.Context, b *types.Ballot) (*tortoise.DecodedBallot, error) {
	t0 := time.Now()
	ref, err := h.checkBallotDataIntegrity(ctx, b)
	if err != nil {
		badData.Inc()
		return nil, err
	}
	ballotDuration.WithLabelValues(dataCheck).Observe(float64(time.Since(t0)))

	t1 := time.Now()
	if err := h.checkBallotDataAvailability(ctx, b); err != nil {
		unavailRef.Inc()
		return nil, err
	}
	ballotDuration.WithLabelValues(fetchRef).Observe(float64(time.Since(t1)))

	t2 := time.Now()
	// ballot can be decoded only if all dependencies (ballots, atxs) were downloaded
	// and added to the tortoise.
	decoded, err := h.tortoise.DecodeBallot(b.ToTortoiseData())
	if err != nil {
		return nil, fmt.Errorf(
			"%w: failed to decode ballot id %s. %v",
			fetch.ErrIgnore,
			b.ID().AsHash32().ShortString(),
			err,
		)
	}
	ballotDuration.WithLabelValues(decode).Observe(float64(time.Since(t2)))

	t3 := time.Now()
	// note that computed opinion has to match signed opinion, otherwise it is unknown
	// if attached votes struct was modified
	//
	// TODO this check can work only on the list with decoded votes, otherwise
	// otherwise it validates only diff, which is easy to bypass
	if err = h.checkVotesConsistency(ctx, b); err != nil {
		badVote.Inc()
		return nil, err
	}
	ballotDuration.WithLabelValues(votes).Observe(float64(time.Since(t3)))

	t4 := time.Now()
	if err := h.validator.CheckEligibility(ctx, b, ref); err != nil {
		notEligible.Inc()
		return nil, err
	}
	ballotDuration.WithLabelValues(eligible).Observe(float64(time.Since(t4)))

	return decoded, nil
}

func (h *Handler) getActiveSetWeight(ctx context.Context, id types.Hash32) (uint64, error) {
	h.weightCalcLock.Lock()
	totalWeight, exists := h.activeSets.Get(id)
	if exists {
		h.weightCalcLock.Unlock()
		return totalWeight, nil
	}

	var ch chan uint64
	chs, exists := h.pendingWeightCalc[id]
	if exists {
		// The calculation is running or the activeset is being fetched,
		// subscribe.
		// Avoid any blocking on the channel by making it buffered, also so that
		// we don't have to wait on it in case the context is canceled
		ch = make(chan uint64, 1)
		h.pendingWeightCalc[id] = append(chs, ch)
		h.weightCalcLock.Unlock()

		// need to wait for the calculation which is already running to finish
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case totalWeight, ok := <-ch:
			if !ok {
				// Channel closed, fetch / calculation failed.
				// The actual error will be logged by the initiator of the
				// initial fetch / calculation, let's not make an
				// impression it happened multiple times and use a simpler
				// message
				return totalWeight, errors.New("error getting activeset weight")
			}
			return totalWeight, nil
		}
	}

	// mark calculation as running
	h.pendingWeightCalc[id] = nil
	h.weightCalcLock.Unlock()

	success := false
	defer func() {
		h.weightCalcLock.Lock()
		// this is guaranteed not to block b/c each channel is buffered
		for _, ch := range h.pendingWeightCalc[id] {
			if success {
				ch <- totalWeight
			}
			close(ch)
		}
		delete(h.pendingWeightCalc, id)
		h.weightCalcLock.Unlock()
	}()

	if err := h.fetcher.GetActiveSet(ctx, id); err != nil {
		return 0, err
	}
	set, err := activesets.Get(h.db, id)
	if err != nil {
		return 0, err
	}
	if len(set.Set) == 0 {
		return 0, fmt.Errorf("%w: empty active set", pubsub.ErrValidationReject)
	}

	computed, used := h.atxsdata.WeightForSet(set.Epoch, set.Set)
	for i := range used {
		if !used[i] {
			return 0, fmt.Errorf(
				"missing atx %s in active set",
				set.Set[i].ShortString(),
			)
		}
	}
	totalWeight = computed
	h.activeSets.Add(id, totalWeight)
	success = true // totalWeight will be sent to the subscribers

	return totalWeight, nil
}

func (h *Handler) checkBallotDataIntegrity(ctx context.Context, b *types.Ballot) (uint64, error) {
	//nolint:nestif
	if b.RefBallot == types.EmptyBallotID {
		// this is the smesher's first Ballot in this epoch, should contain EpochData
		if b.EpochData == nil {
			return 0, fmt.Errorf("%w: missing epoch data", pubsub.ErrValidationReject)
		}
		if b.EpochData.Beacon == types.EmptyBeacon {
			return 0, fmt.Errorf("%w: missing beacon", pubsub.ErrValidationReject)
		}
		epoch := h.clock.CurrentLayer().GetEpoch()
		if epoch > 0 {
			epoch-- // download activesets in the previous epoch too
		}
		if b.Layer.GetEpoch() >= epoch {
			totalWeight, err := h.getActiveSetWeight(ctx, b.EpochData.ActiveSetHash)
			if err != nil {
				return 0, fmt.Errorf("ballot %s: %w", b.ID().String(), err)
			}
			return totalWeight, nil
		}
	} else if b.EpochData != nil {
		return 0, fmt.Errorf("%w: epoch data in non-first ballot %s", pubsub.ErrValidationReject, b.ID())
	}
	return 0, nil
}

func (h *Handler) checkVotesConsistency(ctx context.Context, b *types.Ballot) error {
	exceptions := map[types.BlockID]struct{}{}
	layers := make(map[types.LayerID]types.BlockID)
	// a ballot should not vote for multiple blocks in the same layer within hdist,
	// since hare only output a single block each layer and miner should vote according
	// to the hare output within hdist of the current layer when producing a ballot.
	for _, vote := range b.Votes.Support {
		exceptions[vote.ID] = struct{}{}
		if voted, ok := layers[vote.LayerID]; ok {
			// already voted for a block in this layer
			if voted != vote.ID && vote.LayerID.Add(h.cfg.Hdist).After(b.Layer) {
				h.logger.Warn("ballot doubly voted within hdist, set smesher malicious",
					log.ZContext(ctx),
					zap.Stringer("ballot", b.ID()),
					zap.Uint32("layer", b.Layer.Uint32()),
					zap.Stringer("smesherID", b.SmesherID),
					zap.Stringer("voted_bid", voted),
					zap.Stringer("voted_bid", vote.ID),
					zap.Uint32("hdist", h.cfg.Hdist),
				)
				return errDoubleVoting
			}
		} else {
			layers[vote.LayerID] = vote.ID
		}
	}
	// a ballot should not vote support and against on the same block.
	for _, vote := range b.Votes.Against {
		if _, exist := exceptions[vote.ID]; exist {
			return fmt.Errorf("%w: block %s is referenced multiple times in exceptions of ballot %s at layer %v",
				errConflictingExceptions, vote.ID, b.ID(), b.Layer)
		}
		layers[vote.LayerID] = vote.ID
	}
	if len(exceptions) > h.cfg.MaxExceptions {
		return fmt.Errorf("%w: %d exceptions with max allowed %d in ballot %s",
			errExceptionsOverflow, len(exceptions), h.cfg.MaxExceptions, b.ID())
	}
	// a ballot should not abstain on a layer that it voted for/against on block in that layer.
	for _, lid := range b.Votes.Abstain {
		if _, ok := layers[lid]; ok {
			return fmt.Errorf("%w: conflicting votes %d/%s on layer %d", errConflictingExceptions, b.ID(), b.Layer, lid)
		}
	}
	return nil
}

func (h *Handler) checkBallotDataAvailability(ctx context.Context, b *types.Ballot) error {
	var blts []types.BallotID
	if b.Votes.Base != types.EmptyBallotID && h.tortoise.GetBallot(b.Votes.Base) == nil {
		blts = append(blts, b.Votes.Base)
	}
	if b.RefBallot != types.EmptyBallotID && h.tortoise.GetBallot(b.RefBallot) == nil {
		blts = append(blts, b.RefBallot)
	}
	if err := h.fetcher.GetBallots(ctx, blts); err != nil {
		return fmt.Errorf("fetch ballots: %w", err)
	}
	if h.atxsdata.Get(b.Layer.GetEpoch(), b.AtxID) == nil {
		if err := h.fetcher.GetAtxs(ctx, []types.ATXID{b.AtxID}); err != nil {
			return fmt.Errorf("proposal get ATXs: %w", err)
		}
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
	proposalSize.WithLabelValues().Observe(float64(len(p.SignedBytes())))
	numTxsInProposal.WithLabelValues().Observe(float64(len(p.TxIDs)))
}

func reportVotesMetrics(b *types.Ballot) {
	numBlocksInException.With(prometheus.Labels{diffTypeLabel: diffTypeAgainst}).Observe(float64(len(b.Votes.Against)))
	numBlocksInException.With(prometheus.Labels{diffTypeLabel: diffTypeFor}).Observe(float64(len(b.Votes.Support)))
	numBlocksInException.With(prometheus.Labels{diffTypeLabel: diffTypeNeutral}).Observe(float64(len(b.Votes.Abstain)))
}
