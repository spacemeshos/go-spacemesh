// Package miner is responsible for creating valid blocks that contain valid activation transactions and transactions
package miner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	buildDurationErrorThreshold = 10 * time.Second
)

var (
	errGenesis        = errors.New("not building proposals: genesis")
	errNotSynced      = errors.New("not building proposals: node not synced")
	errNoBeacon       = errors.New("not building proposals: missing beacon")
	errDuplicateLayer = errors.New("not building proposals: duplicate layer event")
)

// ProposalBuilder builds Proposals for a miner.
type ProposalBuilder struct {
	logger log.Log
	cfg    config

	cdb       *datastore.CachedDB
	clock     layerClock
	publisher pubsub.Publisher
	conState  conservativeState
	tortoise  votesEncoder
	syncer    system.SyncStateProvider

	signer  *signing.EdSigner
	session *session

	startOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	eg        errgroup.Group
}

// session per every signing key for the whole epoch.
type session struct {
	epoch     types.EpochID
	beacon    types.Beacon
	atx       types.ATXID
	atxWeight uint64
	ref       types.BallotID
	prev      types.LayerID
	nonce     types.VRFPostIndex
	active    struct {
		set    types.ATXIDList
		weight uint64
	}
	eligibilities struct {
		proofs map[types.LayerID][]types.VotingEligibility
		slots  uint32
	}
}

func (s *session) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("epoch", s.epoch.Uint32())
	encoder.AddString("beacon", s.beacon.String())
	encoder.AddString("atx", s.atx.ShortString())
	encoder.AddUint64("weight", s.atxWeight)
	if s.ref != types.EmptyBallotID {
		encoder.AddString("ref", s.ref.String())
	}
	encoder.AddUint32("prev", s.prev.Uint32())
	if s.eligibilities.proofs != nil {
		encoder.AddUint32("slots", s.eligibilities.slots)
		encoder.AddInt("eligible", len(s.eligibilities.proofs))
		encoder.AddArray("eligible by layer", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			// Sort the layer map to log the layer data in order
			keys := make([]types.LayerID, 0, len(s.eligibilities.proofs))
			for k := range s.eligibilities.proofs {
				keys = append(keys, k)
			}
			sort.Slice(keys, func(i, j int) bool {
				return keys[i].Before(keys[j])
			})
			for _, lyr := range keys {
				encoder.AppendObject(log.ObjectMarshallerFunc(func(encoder log.ObjectEncoder) error {
					encoder.AddUint32("layer", lyr.Uint32())
					encoder.AddInt("slots", len(s.eligibilities.proofs[lyr]))
					return nil
				}))
			}
			return nil
		}))
	}
	return nil
}

// config defines configuration for the ProposalBuilder.
type config struct {
	layerSize          uint32
	layersPerEpoch     uint32
	hdist              uint32
	minActiveSetWeight uint64
	networkDelay       time.Duration

	// used to determine whether a node has enough information on the active set this epoch
	goodAtxPct int
}

func (c *config) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("layer size", c.layerSize)
	encoder.AddUint32("epoch size", c.layersPerEpoch)
	encoder.AddUint32("hdist", c.hdist)
	encoder.AddUint64("min active weight", c.minActiveSetWeight)
	encoder.AddDuration("network delay", c.networkDelay)
	encoder.AddInt("good atx percent", c.goodAtxPct)
	return nil
}

// Opt for configuring ProposalBuilder.
type Opt func(h *ProposalBuilder)

// WithLayerSize defines the average number of proposal per layer.
func WithLayerSize(size uint32) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.layerSize = size
	}
}

// WithLayerPerEpoch defines the number of layers per epoch.
func WithLayerPerEpoch(layers uint32) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.layersPerEpoch = layers
	}
}

func WithMinimalActiveSetWeight(weight uint64) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.minActiveSetWeight = weight
	}
}

// WithLogger defines the logger.
func WithLogger(logger log.Log) Opt {
	return func(pb *ProposalBuilder) {
		pb.logger = logger
	}
}

func WithHdist(dist uint32) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.hdist = dist
	}
}

func WithNetworkDelay(delay time.Duration) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.networkDelay = delay
	}
}

func WithMinGoodAtxPct(pct int) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.goodAtxPct = pct
	}
}

// New creates a struct of block builder type.
func New(
	ctx context.Context,
	clock layerClock,
	signer *signing.EdSigner,
	cdb *datastore.CachedDB,
	publisher pubsub.Publisher,
	trtl votesEncoder,
	syncer system.SyncStateProvider,
	conState conservativeState,
	opts ...Opt,
) *ProposalBuilder {
	sctx, cancel := context.WithCancel(ctx)
	pb := &ProposalBuilder{
		logger:    log.NewNop(),
		ctx:       sctx,
		cancel:    cancel,
		signer:    signer,
		clock:     clock,
		cdb:       cdb,
		publisher: publisher,
		tortoise:  trtl,
		syncer:    syncer,
		conState:  conState,
	}
	for _, opt := range opts {
		opt(pb)
	}
	return pb
}

// Start starts the loop that listens to layers and build proposals.
func (pb *ProposalBuilder) Start(ctx context.Context) error {
	pb.startOnce.Do(func() {
		pb.eg.Go(func() error {
			pb.loop(log.WithNewSessionID(ctx))
			return nil
		})
	})
	return nil
}

// Close stops the loop that listens to layers and build proposals.
func (pb *ProposalBuilder) Close() {
	pb.cancel()
	_ = pb.eg.Wait()
}

// only output the mesh hash in the proposal when the following conditions are met:
// - tortoise has verified every layer i < N-hdist.
// - the node has hare output for every layer i such that N-hdist <= i <= N.
// this is done such that when the node is generating the block based on hare output,
// it can do optimistic filtering if the majority of the proposals agreed on the mesh hash.
func (pb *ProposalBuilder) decideMeshHash(ctx context.Context, current types.LayerID) types.Hash32 {
	var minVerified types.LayerID
	if current.Uint32() > pb.cfg.hdist+1 {
		minVerified = current.Sub(pb.cfg.hdist + 1)
	}
	genesis := types.GetEffectiveGenesis()
	if minVerified.Before(genesis) {
		minVerified = genesis
	}
	verified := pb.tortoise.LatestComplete()
	if minVerified.After(verified) {
		pb.logger.With().Warning("layers outside hdist not verified",
			log.Context(ctx),
			current,
			log.Stringer("min verified", minVerified),
			log.Stringer("latest verified", verified))
		return types.EmptyLayerHash
	}
	pb.logger.With().Debug("verified layer meets optimistic filtering threshold",
		log.Context(ctx),
		current,
		log.Stringer("min verified", minVerified),
		log.Stringer("latest verified", verified),
	)

	for lid := minVerified.Add(1); lid.Before(current); lid = lid.Add(1) {
		_, err := certificates.GetHareOutput(pb.cdb, lid)
		if err != nil {
			pb.logger.With().Warning("missing hare output for layer within hdist",
				log.Context(ctx),
				current,
				log.Stringer("missing_layer", lid),
				log.Err(err),
			)
			return types.EmptyLayerHash
		}
	}
	pb.logger.With().Debug("hare outputs meet optimistic filtering threshold",
		log.Context(ctx),
		current,
		log.Stringer("from", minVerified.Add(1)),
		log.Stringer("to", current.Sub(1)),
	)

	mesh, err := layers.GetAggregatedHash(pb.cdb, current.Sub(1))
	if err != nil {
		pb.logger.With().Warning("failed to get mesh hash",
			log.Context(ctx),
			current,
			log.Err(err),
		)
		return types.EmptyLayerHash
	}
	return mesh
}

func (pb *ProposalBuilder) loadSessionData(ctx context.Context, lid types.LayerID) error {
	if pb.session.nonce == 0 {
		if nonce, err := pb.cdb.VRFNonce(pb.signer.NodeID(), pb.session.epoch); err != nil {
			if errors.Is(err, sql.ErrNotFound) {
				pb.logger.WithContext(ctx).With().Info("miner has no valid vrf nonce, not building proposal", lid)
				return nil
			}
			return err
		} else {
			pb.session.nonce = nonce
		}
	}
	if pb.session.beacon == types.EmptyBeacon {
		if beacon, err := beacons.Get(pb.cdb, pb.session.epoch); err != nil {
			return errNoBeacon
		} else {
			pb.session.beacon = beacon
		}
	}
	if pb.session.atx == types.EmptyATXID {
		atx, err := atxs.GetByEpochAndNodeID(pb.cdb, pb.session.epoch-1, pb.signer.NodeID())
		if err != nil {
			return fmt.Errorf("get atx in epoch %v: %w", pb.session.epoch-1, err)
		}
		pb.session.atx = atx.ID()
		pb.session.atxWeight = atx.GetWeight()
	}
	vrf, err := pb.signer.VRFSigner()
	if err != nil {
		panic(err)
	}
	if pb.session.ref == types.EmptyBallotID {
		ballot, err := ballots.RefBallot(pb.cdb, pb.session.epoch, pb.signer.NodeID())
		if err != nil && errors.Is(err, sql.ErrNotFound) {
			return fmt.Errorf("get refballot %w", err)
		}
		if errors.Is(err, sql.ErrNotFound) {
			weight, set, err := generateActiveSet(
				pb.logger, pb.cdb, vrf, pb.session.epoch, pb.clock.LayerToTime(pb.session.epoch.FirstLayer()),
				pb.cfg.goodAtxPct, pb.cfg.networkDelay, pb.session.atx, pb.session.atxWeight)
			if err != nil {
				return err
			}
			pb.session.active.set = set
			pb.session.active.weight = weight
			slots, err := proposals.GetNumEligibleSlots(pb.session.atxWeight, pb.cfg.minActiveSetWeight, weight, pb.cfg.layerSize, pb.cfg.layersPerEpoch)
			if err != nil {
				return fmt.Errorf("get slots: %w", err)
			}
			pb.session.eligibilities.slots = slots
		} else {
			pb.session.ref = ballot.ID()
			hash := ballot.EpochData.ActiveSetHash
			set, err := activesets.Get(pb.cdb, hash)
			if err != nil {
				return fmt.Errorf("get activeset %s: %w", hash.String(), err)
			}
			var weight uint64
			for _, id := range set.Set {
				atx, err := pb.cdb.GetAtxHeader(id)
				if err != nil {
					return err
				}
				weight += atx.GetWeight()
			}
			pb.session.active.set = set.Set
			pb.session.active.weight = weight
			pb.session.eligibilities.slots = ballot.EpochData.EligibilityCount
		}
	}
	if pb.session.eligibilities.proofs == nil {
		pb.session.eligibilities.proofs = calcEligibilityProofs(vrf, pb.session.epoch, pb.session.beacon, pb.session.nonce, pb.session.eligibilities.slots, pb.cfg.layersPerEpoch)
		pb.logger.With().Info("proposal eligibilities for an epoch", log.Inline(pb.session))
		events.EmitEligibilities(pb.session.epoch, pb.session.beacon, pb.session.atx, uint32(len(pb.session.active.set)), pb.session.eligibilities.proofs)
	}
	return nil
}

func (pb *ProposalBuilder) Build(ctx context.Context, lid types.LayerID) error {
	start := time.Now()

	if lid.FirstInEpoch() {
		pb.session = &session{epoch: lid.GetEpoch()}
	}
	if lid <= types.GetEffectiveGenesis() {
		return errGenesis
	}
	if !pb.syncer.IsSynced(ctx) {
		return errNotSynced
	}
	if err := pb.loadSessionData(ctx, lid); err != nil {
		return err
	}
	if lid <= pb.session.prev {
		return errDuplicateLayer
	}
	pb.session.prev = lid

	proofs := pb.session.eligibilities.proofs[lid]
	if len(proofs) == 0 {
		pb.logger.WithContext(ctx).With().Debug("not eligible for proposal in layer", lid)
		return nil
	}
	pb.logger.WithContext(ctx).With().Debug("eligible for proposals in layer",
		log.Uint32("lid", lid.Uint32()), log.Int("num proposals", len(proofs)),
	)

	pb.tortoise.TallyVotes(ctx, lid)
	// TODO(dshulyak) will get rid from the EncodeVotesWithCurrent option in a followup
	// there are some dependencies in the tests
	opinion, err := pb.tortoise.EncodeVotes(ctx, tortoise.EncodeVotesWithCurrent(lid))
	if err != nil {
		return fmt.Errorf("encode votes: %w", err)
	}
	txs := pb.conState.SelectProposalTXs(lid, len(proofs))
	mesh := pb.decideMeshHash(ctx, lid)
	p := createProposal(ctx, pb.logger, pb.session, pb.signer, lid, txs, *opinion, proofs, mesh)

	elapsed := time.Since(start)
	if elapsed > buildDurationErrorThreshold {
		pb.logger.WithContext(ctx).WithFields(lid, lid.GetEpoch()).With().
			Error("proposal building took too long ", log.Duration("elapsed", elapsed))
	}
	metrics.ProposalBuildDuration.WithLabelValues().Observe(float64(elapsed / time.Millisecond))

	// needs to be saved before publishing, as we will query it in handler
	if pb.session.ref == types.EmptyBallotID {
		if err := activesets.Add(pb.cdb, p.EpochData.ActiveSetHash, &types.EpochActiveSet{
			Epoch: pb.session.epoch,
			Set:   pb.session.active.set,
		}); err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return err
		}
	}

	// generate a new requestID for the new proposal message
	newCtx := log.WithNewRequestID(ctx, lid, p.ID())
	if err = pb.publisher.Publish(newCtx, pubsub.ProposalProtocol, codec.MustEncode(p)); err != nil {
		pb.logger.WithContext(newCtx).With().Error("failed to send proposal", log.Err(err))
	} else {
		events.EmitProposal(lid, p.ID())
		events.ReportProposal(events.ProposalCreated, p)
	}
	return nil
}

func (pb *ProposalBuilder) loop(ctx context.Context) {
	next := pb.clock.CurrentLayer().Add(1)
	pb.logger.With().Info("started", log.Inline(&pb.cfg), log.Uint32("current", next.Uint32()))
	for {
		select {
		case <-pb.ctx.Done():
			return
		case <-pb.clock.AwaitLayer(next):
			current := pb.clock.CurrentLayer()
			if current.Before(next) {
				pb.logger.Info("time sync detected, realigning ProposalBuilder")
				continue
			}
			next = current.Add(1)
			lyrCtx := log.WithNewSessionID(ctx)
			if err := pb.Build(lyrCtx, current); err != nil {
				switch {
				case errors.Is(err, errGenesis), errors.Is(err, errNotSynced):
				default:
					pb.logger.WithContext(lyrCtx).With().Warning("failed to build proposal", current, log.Err(err))
				}
			}
		}
	}
}

func createProposal(
	ctx context.Context,
	logger log.Log,
	session *session,
	signer *signing.EdSigner,
	lid types.LayerID,
	txs []types.TransactionID,
	opinion types.Opinion,
	eligibility []types.VotingEligibility,
	meshHash types.Hash32,
) *types.Proposal {
	ib := &types.InnerBallot{
		Layer:       lid,
		AtxID:       session.atx,
		OpinionHash: opinion.Hash,
	}
	if session.ref == types.EmptyBallotID {
		logger.With().Debug("creating ballot with active set (reference ballot in epoch)",
			log.Context(ctx),
			lid,
			log.Int("active_set_size", len(session.active.set)),
		)
		ib.RefBallot = types.EmptyBallotID
		ib.EpochData = &types.EpochData{
			ActiveSetHash:    session.active.set.Hash(),
			Beacon:           session.beacon,
			EligibilityCount: session.eligibilities.slots,
		}
	} else {
		logger.With().Debug("creating ballot with reference ballot (no active set)",
			log.Context(ctx),
			lid,
			log.Stringer("ref_ballot", session.ref),
		)
		ib.RefBallot = session.ref
	}

	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot:       *ib,
				Votes:             opinion.Votes,
				EligibilityProofs: eligibility,
			},
			TxIDs:    txs,
			MeshHash: meshHash,
		},
	}
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.SmesherID = signer.NodeID()
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	if err := p.Initialize(); err != nil {
		logger.With().Fatal("proposal failed to initialize",
			log.Context(ctx),
			lid,
			log.Err(err),
		)
	}
	logger.With().Info("proposal created",
		log.Context(ctx),
		lid,
		p.ID(),
		log.Int("num txs", len(p.TxIDs)),
	)
	return p
}
