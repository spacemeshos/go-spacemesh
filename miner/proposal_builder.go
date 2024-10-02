// Package miner is responsible for creating valid blocks that contain valid activation transactions and transactions
package miner

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner/minweight"
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

var errAtxNotAvailable = errors.New("atx not available")

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./proposal_builder.go

type conservativeState interface {
	SelectProposalTXs(types.LayerID, int) []types.TransactionID
}

type votesEncoder interface {
	LatestComplete() types.LayerID
	TallyVotes(context.Context, types.LayerID)
	EncodeVotes(context.Context, ...tortoise.EncodeVotesOpts) (*types.Opinion, error)
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

type atxSearch interface {
	GetIDByEpochAndNodeID(ctx context.Context, epoch types.EpochID, nodeID types.NodeID) (types.ATXID, error)
}

type defaultAtxSearch struct {
	db sql.Executor
}

func (p defaultAtxSearch) GetIDByEpochAndNodeID(
	_ context.Context,
	epoch types.EpochID,
	nodeID types.NodeID,
) (types.ATXID, error) {
	return atxs.GetIDByEpochAndNodeID(p.db, epoch, nodeID)
}

// ProposalBuilder builds Proposals for a miner.
type ProposalBuilder struct {
	logger *zap.Logger
	cfg    config

	db        sql.Executor
	localdb   sql.Executor
	atxsdata  *atxsdata.Data
	clock     layerClock
	publisher pubsub.Publisher
	conState  conservativeState
	tortoise  votesEncoder
	syncer    system.SyncStateProvider
	activeGen *activeSetGenerator
	atxs      atxSearch

	signers struct {
		mu      sync.Mutex
		signers map[types.NodeID]*signerSession
	}
	shared sharedSession
}

type signerSession struct {
	signer  *signing.EdSigner
	log     *zap.Logger
	session session
	latency latencyTracker
}

// shared data for all signers in the epoch.
type sharedSession struct {
	epoch  types.EpochID
	beacon types.Beacon
	active struct {
		id     types.Hash32
		set    types.ATXIDList
		weight uint64
	}
}

// session per every signing key for the whole epoch.
type session struct {
	epoch         types.EpochID
	atx           types.ATXID
	atxWeight     uint64
	ref           types.BallotID
	beacon        types.Beacon
	prev          types.LayerID
	nonce         types.VRFPostIndex
	eligibilities struct {
		proofs map[types.LayerID][]types.VotingEligibility
		slots  uint32
	}
}

func (s *session) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
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
		encoder.AddArray(
			"eligible by layer",
			zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
				// Sort the layer map to log the layer data in order
				keys := maps.Keys(s.eligibilities.proofs)
				slices.Sort(keys)
				for _, lyr := range keys {
					encoder.AppendObject(
						zapcore.ObjectMarshalerFunc(func(encoder zapcore.ObjectEncoder) error {
							encoder.AddUint32("layer", lyr.Uint32())
							encoder.AddInt("slots", len(s.eligibilities.proofs[lyr]))
							return nil
						}),
					)
				}
				return nil
			}),
		)
	}
	return nil
}

// config defines configuration for the ProposalBuilder.
type config struct {
	layerSize          uint32
	layersPerEpoch     uint32
	hdist              uint32
	networkDelay       time.Duration
	workersLimit       int
	minActiveSetWeight []types.EpochMinimalActiveWeight
	// used to determine whether a node has enough information on the active set this epoch
	goodAtxPercent int
	activeSet      ActiveSetPreparation
}

// ActiveSetPreparation is a configuration to enable computation of activeset in advance.
type ActiveSetPreparation struct {
	// Window describes how much in advance the active set should be prepared.
	Window time.Duration `mapstructure:"window"`
	// RetryInterval describes how often the active set is retried.
	RetryInterval time.Duration `mapstructure:"retry-interval"`
	// Tries describes how many times the active set is retried.
	Tries int `mapstructure:"tries"`
}

func DefaultActiveSetPreparation() ActiveSetPreparation {
	return ActiveSetPreparation{
		Window:        1 * time.Second,
		RetryInterval: 1 * time.Second,
		Tries:         3,
	}
}

func (c *config) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint32("layer size", c.layerSize)
	encoder.AddUint32("epoch size", c.layersPerEpoch)
	encoder.AddUint32("hdist", c.hdist)
	encoder.AddDuration("network delay", c.networkDelay)
	encoder.AddInt("good atx percent", c.goodAtxPercent)
	encoder.AddDuration("active set window", c.activeSet.Window)
	encoder.AddDuration("active set retry interval", c.activeSet.RetryInterval)
	encoder.AddInt("active set tries", c.activeSet.Tries)
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

// WithWorkersLimit configures paralelization factor for builder operation when working with
// more than one signer.
func WithWorkersLimit(limit int) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.workersLimit = limit
	}
}

// WithLayerPerEpoch defines the number of layers per epoch.
func WithLayerPerEpoch(layers uint32) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.layersPerEpoch = layers
	}
}

func WithMinimalActiveSetWeight(weight []types.EpochMinimalActiveWeight) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.minActiveSetWeight = weight
	}
}

// WithLogger defines the logger.
func WithLogger(logger *zap.Logger) Opt {
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

func WithMinGoodAtxPercent(percent int) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.goodAtxPercent = percent
	}
}

// WithSigners guarantees that builder will start execution with provided list of signers.
// Should be after logging.
func WithSigners(signers ...*signing.EdSigner) Opt {
	return func(pb *ProposalBuilder) {
		for _, signer := range signers {
			pb.Register(signer)
		}
	}
}

// WithActivesetPreparation overwrites configuration for activeset preparation.
func WithActivesetPreparation(prep ActiveSetPreparation) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.activeSet = prep
	}
}

func withAtxSearch(p atxSearch) Opt {
	return func(pb *ProposalBuilder) {
		pb.atxs = p
	}
}

// New creates a struct of block builder type.
func New(
	clock layerClock,
	db sql.Executor,
	localdb sql.Executor,
	atxsdata *atxsdata.Data,
	publisher pubsub.Publisher,
	trtl votesEncoder,
	syncer system.SyncStateProvider,
	conState conservativeState,
	opts ...Opt,
) *ProposalBuilder {
	pb := &ProposalBuilder{
		cfg: config{
			workersLimit: runtime.NumCPU(),
			activeSet:    DefaultActiveSetPreparation(),
		},
		logger:    zap.NewNop(),
		clock:     clock,
		db:        db,
		localdb:   localdb,
		atxsdata:  atxsdata,
		publisher: publisher,
		tortoise:  trtl,
		syncer:    syncer,
		conState:  conState,
		atxs:      defaultAtxSearch{db},
		signers: struct {
			mu      sync.Mutex
			signers map[types.NodeID]*signerSession
		}{
			signers: map[types.NodeID]*signerSession{},
		},
	}
	for _, opt := range opts {
		opt(pb)
	}
	pb.activeGen = newActiveSetGenerator(pb.cfg, pb.logger, pb.db, pb.localdb, pb.atxsdata, pb.clock)
	return pb
}

func (pb *ProposalBuilder) Register(sig *signing.EdSigner) {
	pb.signers.mu.Lock()
	defer pb.signers.mu.Unlock()
	_, exist := pb.signers.signers[sig.NodeID()]
	if !exist {
		pb.logger.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
		pb.signers.signers[sig.NodeID()] = &signerSession{
			signer: sig,
			log:    pb.logger.With(zap.String("signer", sig.NodeID().ShortString())),
		}
	}
}

// Start the loop that listens to layers and build proposals.
func (pb *ProposalBuilder) Run(ctx context.Context) error {
	current := pb.clock.CurrentLayer()
	next := current + 1
	pb.logger.Info("started", zap.Inline(&pb.cfg), zap.Uint32("next", next.Uint32()))
	var eg errgroup.Group
	prepareDisabled := pb.cfg.activeSet.Tries == 0 || pb.cfg.activeSet.RetryInterval == 0
	if prepareDisabled {
		pb.logger.Warn("activeset will not be prepared in advance")
	}
	for {
		select {
		case <-ctx.Done():
			eg.Wait()
			return nil
		case <-pb.clock.AwaitLayer(next):
			current := pb.clock.CurrentLayer()
			if current.Before(next) {
				pb.logger.Info("time sync detected, realigning ProposalBuilder",
					zap.Uint32("current", current.Uint32()),
					zap.Uint32("next", next.Uint32()),
				)
				continue
			}
			next = current.Add(1)
			ctx := log.WithNewSessionID(ctx)
			synced := pb.syncer.IsSynced(ctx)
			if !prepareDisabled {
				eg.Go(func() error {
					pb.runPrepare(ctx, current)
					return nil
				})
				prepareDisabled = true
			}
			if current <= types.GetEffectiveGenesis() || !synced {
				continue
			}
			if err := pb.build(ctx, current); err != nil {
				pb.logger.Warn("failed to build proposal",
					log.ZContext(ctx),
					zap.Uint32("lid", current.Uint32()),
					zap.Error(err),
				)
			}
		}
	}
}

func (pb *ProposalBuilder) runPrepare(ctx context.Context, current types.LayerID) {
	nextEpoch := current.GetEpoch() + 1
	wait := time.Until(
		pb.clock.LayerToTime((nextEpoch).FirstLayer()).Add(-pb.cfg.activeSet.Window))
	if wait > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
	pb.activeGen.ensure(ctx, nextEpoch)
}

// Only output the mesh hash in the proposal when the following conditions are met:
// - tortoise has verified every layer i < N-hdist.
// - the node has hare output for every layer i such that N-hdist <= i <= N.
// This is done such that when the node is generating the block based on hare output,
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
		pb.logger.Warn("layers outside hdist not verified",
			log.ZContext(ctx),
			zap.Uint32("layer_id", current.Uint32()),
			zap.Stringer("min verified", minVerified),
			zap.Stringer("latest verified", verified),
		)
		return types.EmptyLayerHash
	}
	pb.logger.Debug("verified layer meets optimistic filtering threshold",
		log.ZContext(ctx),
		zap.Uint32("layer_id", current.Uint32()),
		zap.Stringer("min verified", minVerified),
		zap.Stringer("latest verified", verified),
	)

	for lid := minVerified.Add(1); lid.Before(current); lid = lid.Add(1) {
		_, err := certificates.GetHareOutput(pb.db, lid)
		if err != nil {
			pb.logger.Warn("missing hare output for layer within hdist",
				log.ZContext(ctx),
				zap.Uint32("layer_id", current.Uint32()),
				zap.Stringer("missing_layer", lid),
				zap.Error(err),
			)
			return types.EmptyLayerHash
		}
	}
	pb.logger.Debug("hare outputs meet optimistic filtering threshold",
		log.ZContext(ctx),
		zap.Uint32("layer_id", current.Uint32()),
		zap.Stringer("from", minVerified.Add(1)),
		zap.Stringer("to", current.Sub(1)),
	)

	mesh, err := layers.GetAggregatedHash(pb.db, current.Sub(1))
	if err != nil {
		pb.logger.Warn("failed to get mesh hash",
			log.ZContext(ctx),
			zap.Uint32("layer_id", current.Uint32()),
			zap.Error(err),
		)
		return types.EmptyLayerHash
	}
	return mesh
}

func (pb *ProposalBuilder) UpdateActiveSet(target types.EpochID, set []types.ATXID) {
	pb.activeGen.updateFallback(target, set)
}

func (pb *ProposalBuilder) initSharedData(current types.LayerID) error {
	if pb.shared.epoch != current.GetEpoch() {
		pb.shared = sharedSession{epoch: current.GetEpoch()}
	}
	if pb.shared.beacon == types.EmptyBeacon {
		beacon, err := beacons.Get(pb.db, pb.shared.epoch)
		if err != nil || beacon == types.EmptyBeacon {
			return fmt.Errorf("missing beacon for epoch %d", pb.shared.epoch)
		}
		pb.shared.beacon = beacon
	}
	if pb.shared.active.set != nil {
		return nil
	}
	id, weight, set, err := pb.activeGen.generate(current, current.GetEpoch())
	if err != nil {
		return err
	}
	pb.logger.Debug("loaded prepared active set",
		zap.Uint32("epoch_id", pb.shared.epoch.Uint32()),
		log.ZShortStringer("id", id),
		zap.Int("size", len(set)),
		zap.Uint64("weight", weight),
	)
	pb.shared.active.id = id
	pb.shared.active.set = set
	pb.shared.active.weight = weight
	return nil
}

func (pb *ProposalBuilder) initSignerData(ctx context.Context, ss *signerSession, lid types.LayerID) error {
	if ss.session.epoch != lid.GetEpoch() {
		ss.session = session{epoch: lid.GetEpoch()}
	}
	if ss.session.atx == types.EmptyATXID {
		atxid, err := pb.atxs.GetIDByEpochAndNodeID(ctx, ss.session.epoch-1, ss.signer.NodeID())
		switch {
		case errors.Is(err, sql.ErrNotFound):
			return errAtxNotAvailable
		case err != nil:
			return fmt.Errorf("get atx in epoch %v: %w", ss.session.epoch-1, err)
		}
		atx := pb.atxsdata.Get(ss.session.epoch, atxid)
		if atx == nil {
			return fmt.Errorf("missing atx in atxsdata %v", atxid)
		}
		ss.session.atx = atxid
		ss.session.atxWeight = atx.Weight
		ss.session.nonce = atx.Nonce
	}
	if ss.session.prev == 0 {
		prev, err := ballots.LastInEpoch(pb.db, ss.session.atx, ss.session.epoch)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return err
		}
		if err == nil {
			ss.session.prev = prev.Layer
		}
	}
	if ss.session.ref == types.EmptyBallotID {
		ballot, err := ballots.FirstInEpoch(pb.db, ss.session.atx, ss.session.epoch)
		if err != nil && !errors.Is(err, sql.ErrNotFound) {
			return fmt.Errorf("get refballot %w", err)
		}
		if errors.Is(err, sql.ErrNotFound) {
			ss.session.beacon = pb.shared.beacon
			ss.session.eligibilities.slots = proposals.MustGetNumEligibleSlots(
				ss.session.atxWeight,
				minweight.Select(lid.GetEpoch(), pb.cfg.minActiveSetWeight),
				pb.shared.active.weight,
				pb.cfg.layerSize,
				pb.cfg.layersPerEpoch,
			)
		} else {
			if ballot.EpochData == nil {
				return fmt.Errorf("atx %d created invalid first ballot", ss.session.atx)
			}
			ss.session.ref = ballot.ID()
			ss.session.beacon = ballot.EpochData.Beacon
			ss.session.eligibilities.slots = ballot.EpochData.EligibilityCount
		}
	}
	if ss.session.eligibilities.proofs == nil {
		ss.session.eligibilities.proofs = calcEligibilityProofs(
			ss.signer.VRFSigner(),
			ss.session.epoch,
			ss.session.beacon,
			ss.session.nonce,
			ss.session.eligibilities.slots,
			pb.cfg.layersPerEpoch,
		)
		ss.log.Info("proposal eligibilities for an epoch", zap.Inline(&ss.session))
		events.EmitEligibilities(
			ss.signer.NodeID(),
			ss.session.epoch,
			ss.session.beacon,
			ss.session.atx,
			uint32(len(pb.shared.active.set)),
			ss.session.eligibilities.proofs,
		)
	}
	return nil
}

func (pb *ProposalBuilder) build(ctx context.Context, lid types.LayerID) error {
	buildStartTime := time.Now()
	if err := pb.initSharedData(lid); err != nil {
		return err
	}

	// don't accept registration in the middle of computing proposals
	pb.signers.mu.Lock()
	signers := maps.Values(pb.signers.signers)
	pb.signers.mu.Unlock()

	encodeVotesOnce := sync.OnceValues(func() (*types.Opinion, error) {
		pb.tortoise.TallyVotes(ctx, lid)
		// TODO(dshulyak) get rid from the EncodeVotesWithCurrent option in a followup
		// there are some dependencies in the tests
		opinion, err := pb.tortoise.EncodeVotes(ctx, tortoise.EncodeVotesWithCurrent(lid))
		if err != nil {
			return nil, fmt.Errorf("encoding votes: %w", err)
		}
		return opinion, nil
	})

	calcMeshHashOnce := sync.OnceValue(func() types.Hash32 {
		start := time.Now()
		meshHash := pb.decideMeshHash(ctx, lid)
		duration := time.Since(start)
		for _, ss := range signers {
			ss.latency.hash = duration
		}
		return meshHash
	})

	persistActiveSetOnce := sync.OnceValue(func() error {
		err := activesets.Add(pb.db, pb.shared.active.id, &types.EpochActiveSet{
			Epoch: pb.shared.epoch,
			Set:   pb.shared.active.set,
		})
		if err != nil && !errors.Is(err, sql.ErrObjectExists) {
			return err
		}
		return nil
	})

	// Two stage pipeline, with the stages running in parallel.
	// 1. Initializes signers. Runs limited number of goroutines because the initialization is CPU and DB bound.
	// 2. Collects eligible signers' sessions from the stage 1 and creates and publishes proposals.

	// Used to pass eligible singers from stage 1 → 2.
	// Buffered with capacity for all signers so that writes don't block.
	eligible := make(chan *signerSession, len(signers))

	// Stage 1
	// Use a semaphore instead of eg.SetLimit so that the stage 2 starts immediately after
	// scheduling all signers in the stage 1. Otherwise, stage 2 would wait for all stage 1
	// goroutines to at least start, which is not what we want. We want to start stage 2 as soon as possible.
	limiter := semaphore.NewWeighted(int64(pb.cfg.workersLimit))
	var eg errgroup.Group
	for _, ss := range signers {
		eg.Go(func() error {
			if err := limiter.Acquire(ctx, 1); err != nil {
				return err
			}
			defer limiter.Release(1)

			start := time.Now()
			ss.latency.start = buildStartTime
			if err := pb.initSignerData(ctx, ss, lid); err != nil {
				if errors.Is(err, errAtxNotAvailable) {
					ss.log.Debug("smesher doesn't have atx that targets this epoch",
						log.ZContext(ctx),
						zap.Uint32("epoch_id", ss.session.epoch.Uint32()),
					)
				} else {
					return err
				}
			}
			ss.latency.data = time.Since(start)
			if lid <= ss.session.prev {
				return fmt.Errorf("layer %d was already built by signer %s", lid, ss.signer.NodeID().ShortString())
			}
			ss.session.prev = lid
			proofs := ss.session.eligibilities.proofs[lid]
			if len(proofs) == 0 {
				ss.log.Debug("not eligible for proposal in layer",
					log.ZContext(ctx),
					zap.Uint32("layer_id", lid.Uint32()),
					zap.Uint32("epoch_id", lid.GetEpoch().Uint32()),
				)
				return nil
			}
			ss.log.Debug("eligible for proposals in layer",
				log.ZContext(ctx),
				zap.Uint32("layer_id", lid.Uint32()),
				zap.Uint32("epoch_id", lid.GetEpoch().Uint32()),
				zap.Int("num proposals", len(proofs)),
			)
			eligible <- ss // won't block
			return nil
		})
	}

	var stage1Err error
	go func() {
		stage1Err = eg.Wait()
		close(eligible)
	}()

	// Stage 2
	eg2 := errgroup.Group{}
	for ss := range eligible {
		start := time.Now()
		opinion, err := encodeVotesOnce()
		if err != nil {
			return err
		}
		ss.latency.tortoise = time.Since(start)

		start = time.Now()
		meshHash := calcMeshHashOnce()
		ss.latency.hash = time.Since(start)

		eg2.Go(func() error {
			// needs to be saved before publishing, as we will query it in handler
			if ss.session.ref == types.EmptyBallotID {
				start := time.Now()
				if err := persistActiveSetOnce(); err != nil {
					return err
				}
				ss.latency.activeSet = time.Since(start)
			}
			proofs := ss.session.eligibilities.proofs[lid]

			start = time.Now()
			txs := pb.conState.SelectProposalTXs(lid, len(proofs))
			ss.latency.txs = time.Since(start)

			start = time.Now()
			proposal := createProposal(
				&ss.session,
				pb.shared.beacon,
				pb.shared.active.set,
				ss.signer,
				lid,
				txs,
				opinion,
				proofs,
				meshHash,
			)
			if err := pb.publisher.Publish(ctx, pubsub.ProposalProtocol, codec.MustEncode(proposal)); err != nil {
				ss.log.Error("failed to publish proposal",
					log.ZContext(ctx),
					zap.Uint32("lid", proposal.Layer.Uint32()),
					zap.Stringer("id", proposal.ID()),
					zap.Error(err),
				)
			} else {
				ss.latency.publish = time.Since(start)
				ss.latency.end = time.Now()
				ss.log.Info("proposal created",
					log.ZContext(ctx),
					zap.Inline(proposal),
					zap.Object("latency", &ss.latency),
				)
				proposalBuild.Observe(ss.latency.total().Seconds())
				events.EmitProposal(ss.signer.NodeID(), lid, proposal.ID())
				events.ReportProposal(events.ProposalCreated, proposal)
			}
			return nil
		})
	}

	return errors.Join(stage1Err, eg2.Wait())
}

func createProposal(
	session *session,
	beacon types.Beacon,
	activeset types.ATXIDList,
	signer *signing.EdSigner,
	lid types.LayerID,
	txs []types.TransactionID,
	opinion *types.Opinion,
	eligibility []types.VotingEligibility,
	meshHash types.Hash32,
) *types.Proposal {
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: types.InnerBallot{
					Layer:       lid,
					AtxID:       session.atx,
					OpinionHash: opinion.Hash,
				},
				Votes:             opinion.Votes,
				EligibilityProofs: eligibility,
			},
			TxIDs:    txs,
			MeshHash: meshHash,
		},
	}
	if session.ref == types.EmptyBallotID {
		p.Ballot.RefBallot = types.EmptyBallotID
		p.Ballot.EpochData = &types.EpochData{
			ActiveSetHash:    activeset.Hash(),
			Beacon:           beacon,
			EligibilityCount: session.eligibilities.slots,
		}
	} else {
		p.Ballot.RefBallot = session.ref
	}
	p.Ballot.Signature = signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.SmesherID = signer.NodeID()
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	p.MustInitialize()
	return p
}

// calcEligibilityProofs calculates the eligibility proofs of proposals for the miner in the given epoch
// and returns the proofs along with the epoch's active set.
func calcEligibilityProofs(
	signer *signing.VRFSigner,
	epoch types.EpochID,
	beacon types.Beacon,
	nonce types.VRFPostIndex,
	slots uint32,
	layersPerEpoch uint32,
) map[types.LayerID][]types.VotingEligibility {
	proofs := make(map[types.LayerID][]types.VotingEligibility, slots)
	for counter := range slots {
		vrf := signer.Sign(proposals.MustSerializeVRFMessage(beacon, epoch, nonce, counter))
		layer := proposals.CalcEligibleLayer(epoch, layersPerEpoch, vrf)
		proofs[layer] = append(proofs[layer], types.VotingEligibility{
			J:   counter,
			Sig: vrf,
		})
	}
	return proofs
}
