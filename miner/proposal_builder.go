// Package miner is responsible for creating valid blocks that contain valid activation transactions and transactions
package miner

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
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
	cdb    *datastore.CachedDB

	startOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	eg        errgroup.Group

	clock          layerClock
	publisher      pubsub.Publisher
	signer         *signing.EdSigner
	nonceFetcher   nonceFetcher
	conState       conservativeState
	tortoise       votesEncoder
	proposalOracle proposalOracle
	beaconProvider system.BeaconGetter
	syncer         system.SyncStateProvider
}

// config defines configuration for the ProposalBuilder.
type config struct {
	layerSize      uint32
	layersPerEpoch uint32
	hdist          uint32
	nodeID         types.NodeID
}

type defaultFetcher struct {
	cdb *datastore.CachedDB
}

func (f defaultFetcher) VRFNonce(nodeID types.NodeID, epoch types.EpochID) (types.VRFPostIndex, error) {
	nonce, err := f.cdb.VRFNonce(nodeID, epoch)
	if err != nil {
		return types.VRFPostIndex(0), fmt.Errorf("get vrf nonce: %w", err)
	}
	return nonce, nil
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

// WithNodeID defines the miner's NodeID.
func WithNodeID(id types.NodeID) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.nodeID = id
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

func withOracle(o proposalOracle) Opt {
	return func(pb *ProposalBuilder) {
		pb.proposalOracle = o
	}
}

func withNonceFetcher(nf nonceFetcher) Opt {
	return func(pb *ProposalBuilder) {
		pb.nonceFetcher = nf
	}
}

// NewProposalBuilder creates a struct of block builder type.
func NewProposalBuilder(
	ctx context.Context,
	clock layerClock,
	signer *signing.EdSigner,
	vrfSigner *signing.VRFSigner,
	cdb *datastore.CachedDB,
	publisher pubsub.Publisher,
	trtl votesEncoder,
	beaconProvider system.BeaconGetter,
	syncer system.SyncStateProvider,
	conState conservativeState,
	opts ...Opt,
) *ProposalBuilder {
	sctx, cancel := context.WithCancel(ctx)
	pb := &ProposalBuilder{
		logger:         log.NewNop(),
		ctx:            sctx,
		cancel:         cancel,
		signer:         signer,
		clock:          clock,
		cdb:            cdb,
		publisher:      publisher,
		tortoise:       trtl,
		beaconProvider: beaconProvider,
		syncer:         syncer,
		conState:       conState,
	}

	for _, opt := range opts {
		opt(pb)
	}

	if pb.proposalOracle == nil {
		pb.proposalOracle = newMinerOracle(pb.cfg.layerSize, pb.cfg.layersPerEpoch, cdb, vrfSigner, pb.cfg.nodeID, pb.logger)
	}

	if pb.nonceFetcher == nil {
		pb.nonceFetcher = defaultFetcher{pb.cdb}
	}

	return pb
}

// Start starts the loop that listens to layers and build proposals.
func (pb *ProposalBuilder) Start(ctx context.Context) error {
	pb.startOnce.Do(func() {
		pb.eg.Go(func() error {
			pb.createProposalLoop(log.WithNewSessionID(ctx))
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

// stopped returns if we should stop.
func (pb *ProposalBuilder) stopped() bool {
	select {
	case <-pb.ctx.Done():
		return true
	default:
		return false
	}
}

func (pb *ProposalBuilder) createProposal(
	ctx context.Context,
	layerID types.LayerID,
	epochEligibility *EpochEligibility,
	beacon types.Beacon,
	txIDs []types.TransactionID,
	opinion types.Opinion,
) (*types.Proposal, error) {
	logger := pb.logger.WithContext(ctx).WithFields(layerID, layerID.GetEpoch())

	if !layerID.After(types.GetEffectiveGenesis()) {
		logger.With().Fatal("attempt to create proposal during genesis", layerID)
	}

	ib := &types.InnerBallot{
		Layer:       layerID,
		AtxID:       epochEligibility.Atx,
		OpinionHash: opinion.Hash,
	}

	epoch := layerID.GetEpoch()
	refBallot, err := ballots.GetRefBallot(pb.cdb, epoch, pb.signer.NodeID())
	if err != nil {
		if !errors.Is(err, sql.ErrNotFound) {
			logger.With().Error("failed to get ref ballot", log.Err(err))
			return nil, fmt.Errorf("get ref ballot: %w", err)
		}

		logger.With().Debug("creating ballot with active set (reference ballot in epoch)",
			log.Int("active_set_size", len(epochEligibility.ActiveSet)))
		ib.RefBallot = types.EmptyBallotID
		ib.EpochData = &types.EpochData{
			ActiveSetHash:    epochEligibility.ActiveSet.Hash(),
			Beacon:           beacon,
			EligibilityCount: epochEligibility.Slots,
		}
	} else {
		logger.With().Debug("creating ballot with reference ballot (no active set)",
			log.Named("ref_ballot", refBallot))
		ib.RefBallot = refBallot
	}

	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot:       *ib,
				Votes:             opinion.Votes,
				EligibilityProofs: epochEligibility.Proofs[layerID],
			},
			TxIDs:    txIDs,
			MeshHash: pb.decideMeshHash(logger, layerID),
		},
	}
	if p.EpochData != nil {
		p.ActiveSet = epochEligibility.ActiveSet
	}
	p.Ballot.Signature = pb.signer.Sign(signing.BALLOT, p.Ballot.SignedBytes())
	p.SmesherID = pb.signer.NodeID()
	p.Signature = pb.signer.Sign(signing.BALLOT, p.SignedBytes())
	if err := p.Initialize(); err != nil {
		logger.With().Fatal("proposal failed to initialize", log.Err(err))
	}
	logger.Event().Info("proposal created", p.ID(), log.Int("num_txs", len(p.TxIDs)))
	return p, nil
}

// only output the mesh hash in the proposal when the following conditions are met:
// - tortoise has verified every layer i < N-hdist.
// - the node has hare output for every layer i such that N-hdist <= i <= N.
// this is done such that when the node is generating the block based on hare output,
// it can do optimistic filtering if the majority of the proposals agreed on the mesh hash.
func (pb *ProposalBuilder) decideMeshHash(logger log.Log, current types.LayerID) types.Hash32 {
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
		logger.With().Warning("layers outside hdist not verified",
			log.Stringer("min_verified", minVerified),
			log.Stringer("latest_verified", verified))
		return types.EmptyLayerHash
	}
	logger.With().Debug("verified layer meets optimistic filtering threshold",
		log.Stringer("min_verified", minVerified),
		log.Stringer("latest_verified", verified))

	for lid := minVerified.Add(1); lid.Before(current); lid = lid.Add(1) {
		_, err := certificates.GetHareOutput(pb.cdb, lid)
		if err != nil {
			logger.With().Warning("missing hare output for layer within hdist",
				log.Stringer("missing_layer", lid),
				log.Err(err))
			return types.EmptyLayerHash
		}
	}
	logger.With().Debug("hare outputs meet optimistic filtering threshold",
		log.Stringer("from", minVerified.Add(1)),
		log.Stringer("to", current.Sub(1)))

	mesh, err := layers.GetAggregatedHash(pb.cdb, current.Sub(1))
	if err != nil {
		logger.With().Warning("failed to get mesh hash", log.Err(err))
		return types.EmptyLayerHash
	}
	return mesh
}

func (pb *ProposalBuilder) handleLayer(ctx context.Context, layerID types.LayerID) error {
	var (
		beacon types.Beacon
		err    error
		epoch  = layerID.GetEpoch()
		logger = pb.logger.WithContext(ctx).WithFields(layerID, epoch)
	)

	if layerID.GetEpoch().IsGenesis() {
		logger.Info("not building proposal: genesis")
		return errGenesis
	}
	if !pb.syncer.IsSynced(ctx) {
		logger.Info("not building proposal: not synced")
		return errNotSynced
	}
	if beacon, err = pb.beaconProvider.GetBeacon(epoch); err != nil {
		logger.With().Warning("beacon not available for epoch", log.Err(err))
		return errNoBeacon
	}

	started := time.Now()

	count, err := ballots.CountByPubkeyLayer(pb.cdb, layerID, pb.signer.NodeID())
	if err != nil {
		logger.With().Error("count ballots in a layer for public key", log.Err(err))
		return err
	} else if count != 0 {
		logger.With().Error("smesher already created a proposal in this layer",
			log.Int("count", count),
		)
		return errDuplicateLayer
	}

	nonce, err := pb.nonceFetcher.VRFNonce(pb.signer.NodeID(), layerID.GetEpoch())
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			logger.With().Info("miner has no valid vrf nonce, not building proposal")
			return nil
		} else {
			logger.With().Error("failed to get VRF nonce", log.Err(err))
		}
		return err
	}
	epochEligibility, err := pb.proposalOracle.GetProposalEligibility(layerID, beacon, nonce)
	if err != nil {
		if errors.Is(err, errMinerHasNoATXInPreviousEpoch) {
			logger.Info("miner has no ATX in previous epoch")
			return fmt.Errorf("miner no ATX: %w", err)
		}
		logger.With().Error("failed to check for proposal eligibility", log.Err(err))
		return fmt.Errorf("proposal eligibility: %w", err)
	}
	proofs := epochEligibility.Proofs[layerID]
	if len(proofs) == 0 {
		logger.Debug("not eligible for proposal in layer")
		return nil
	}
	logger.With().Info("eligible for proposals in layer",
		epochEligibility.Atx,
		log.Int("num_proposals", len(proofs)),
	)

	pb.tortoise.TallyVotes(ctx, layerID)
	// TODO(dshulyak) will get rid from the EncodeVotesWithCurrent option in a followup
	// there are some dependencies in the tests
	opinion, err := pb.tortoise.EncodeVotes(ctx, tortoise.EncodeVotesWithCurrent(layerID))
	if err != nil {
		return fmt.Errorf("get base ballot: %w", err)
	}

	txList := pb.conState.SelectProposalTXs(layerID, len(proofs))
	p, err := pb.createProposal(ctx, layerID, epochEligibility, beacon, txList, *opinion)
	if err != nil {
		logger.With().Error("failed to create new proposal", log.Err(err))
		return err
	}

	pb.saveMetrics(ctx, started, layerID)

	if pb.stopped() {
		return nil
	}

	pb.eg.Go(func() error {
		// generate a new requestID for the new proposal message
		newCtx := log.WithNewRequestID(ctx, layerID, p.ID())
		// validation handler, where proposal is persisted, is applied synchronously before
		// proposal is sent over the network
		data, err := codec.Encode(p)
		if err != nil {
			logger.With().Panic("failed to serialize proposal", log.Err(err))
		}
		if err = pb.publisher.Publish(newCtx, pubsub.ProposalProtocol, data); err != nil {
			logger.WithContext(newCtx).With().Error("failed to send proposal", log.Err(err))
		}
		events.ReportProposal(events.ProposalCreated, p)
		return nil
	})
	return nil
}

func (pb *ProposalBuilder) createProposalLoop(ctx context.Context) {
	next := pb.clock.CurrentLayer().Add(1)
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
			_ = pb.handleLayer(lyrCtx, current)
		}
	}
}

func (pb *ProposalBuilder) saveMetrics(ctx context.Context, started time.Time, layerID types.LayerID) {
	elapsed := time.Since(started)
	if elapsed > buildDurationErrorThreshold {
		pb.logger.WithContext(ctx).WithFields(layerID, layerID.GetEpoch()).With().
			Error("proposal building took too long ", log.Duration("elapsed", elapsed))
	}

	metrics.ProposalBuildDuration.WithLabelValues().Observe(float64(elapsed / time.Millisecond))
}
