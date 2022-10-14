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
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	buildDurationErrorThreshold = 10 * time.Second
)

var (
	errGenesis   = errors.New("not building proposals: genesis")
	errNotSynced = errors.New("not building proposals: node not synced")
	errNoBeacon  = errors.New("not building proposals: missing beacon")
)

// ProposalBuilder builds Proposals for a miner.
type ProposalBuilder struct {
	logger log.Log
	cfg    config
	cdb    *datastore.CachedDB

	startOnce  sync.Once
	ctx        context.Context
	cancel     context.CancelFunc
	eg         errgroup.Group
	layerTimer chan types.LayerID

	publisher          pubsub.Publisher
	signer             *signing.EdSigner
	conState           conservativeState
	baseBallotProvider votesEncoder
	proposalOracle     proposalOracle
	beaconProvider     system.BeaconGetter
	syncer             system.SyncStateProvider
}

// config defines configuration for the ProposalBuilder.
type config struct {
	layerSize      uint32
	layersPerEpoch uint32
	minerID        types.NodeID
	txsPerProposal int
}

// defaultConfig for BlockHandler.
func defaultConfig() config {
	return config{
		txsPerProposal: 100,
	}
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

// WithMinerID defines the miner's NodeID.
func WithMinerID(id types.NodeID) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.minerID = id
	}
}

// WithTxsPerProposal defines the number of TXs in a Proposal.
func WithTxsPerProposal(numTxs int) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.txsPerProposal = numTxs
	}
}

// WithLogger defines the logger.
func WithLogger(logger log.Log) Opt {
	return func(pb *ProposalBuilder) {
		pb.logger = logger
	}
}

func withOracle(o proposalOracle) Opt {
	return func(pb *ProposalBuilder) {
		pb.proposalOracle = o
	}
}

// NewProposalBuilder creates a struct of block builder type.
func NewProposalBuilder(
	ctx context.Context,
	layerTimer timesync.LayerTimer,
	signer *signing.EdSigner,
	vrfSigner *signing.VRFSigner,
	cdb *datastore.CachedDB,
	publisher pubsub.Publisher,
	bbp votesEncoder,
	beaconProvider system.BeaconGetter,
	syncer system.SyncStateProvider,
	conState conservativeState,
	opts ...Opt,
) *ProposalBuilder {
	sctx, cancel := context.WithCancel(ctx)
	pb := &ProposalBuilder{
		logger:             log.NewNop(),
		cfg:                defaultConfig(),
		ctx:                sctx,
		cancel:             cancel,
		signer:             signer,
		layerTimer:         layerTimer,
		cdb:                cdb,
		publisher:          publisher,
		baseBallotProvider: bbp,
		beaconProvider:     beaconProvider,
		syncer:             syncer,
		conState:           conState,
	}

	for _, opt := range opts {
		opt(pb)
	}

	if pb.proposalOracle == nil {
		pb.proposalOracle = newMinerOracle(pb.cfg.layerSize, pb.cfg.layersPerEpoch, cdb, vrfSigner, pb.cfg.minerID, pb.logger)
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
	proofs []types.VotingEligibilityProof,
	atxID types.ATXID,
	activeSet []types.ATXID,
	beacon types.Beacon,
	txIDs []types.TransactionID,
	opinion types.Opinion,
) (*types.Proposal, error) {
	logger := pb.logger.WithContext(ctx).WithFields(layerID, layerID.GetEpoch())

	if !layerID.After(types.GetEffectiveGenesis()) {
		logger.Panic("attempt to create proposal during genesis")
	}

	ib := &types.InnerBallot{
		AtxID:             atxID,
		EligibilityProofs: proofs,
		LayerIndex:        layerID,
		OpinionHash:       opinion.Hash,
	}

	epoch := layerID.GetEpoch()
	refBallot, err := ballots.GetRefBallot(pb.cdb, epoch, pb.signer.PublicKey().Bytes())
	if err != nil {
		if !errors.Is(err, sql.ErrNotFound) {
			logger.With().Error("failed to get ref ballot", log.Err(err))
			return nil, fmt.Errorf("get ref ballot: %w", err)
		}

		logger.With().Debug("creating ballot with active set (reference ballot in epoch)",
			log.Int("active_set_size", len(activeSet)))
		ib.RefBallot = types.EmptyBallotID
		ib.EpochData = &types.EpochData{
			ActiveSet: activeSet,
			Beacon:    beacon,
		}
	} else {
		logger.With().Debug("creating ballot with reference ballot (no active set)",
			log.Named("ref_ballot", refBallot))
		ib.RefBallot = refBallot
	}

	mesh, err := layers.GetAggregatedHash(pb.cdb, layerID.Sub(1))
	if err != nil {
		logger.With().Warning("failed to get mesh hash", log.Err(err))
	}
	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: *ib,
				Votes:       opinion.Votes,
			},
			TxIDs:    txIDs,
			MeshHash: mesh,
		},
	}
	p.Ballot.Signature = pb.signer.Sign(p.Ballot.Bytes())
	p.Signature = pb.signer.Sign(p.Bytes())
	if err := p.Initialize(); err != nil {
		logger.Panic("proposal failed to initialize", log.Err(err))
	}
	logger.Event().Info("proposal created", p.ID(), log.Int("num_txs", len(p.TxIDs)))
	return p, nil
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

	logger.With().Info("miner got beacon to build proposals", beacon)

	started := time.Now()

	atxID, activeSet, proofs, err := pb.proposalOracle.GetProposalEligibility(layerID, beacon)
	if err != nil {
		if errors.Is(err, errMinerHasNoATXInPreviousEpoch) {
			logger.Info("miner has no ATX in previous epoch")
			return fmt.Errorf("miner no ATX: %w", err)
		}
		logger.With().Error("failed to check for proposal eligibility", log.Err(err))
		return fmt.Errorf("proposal eligibility: %w", err)
	}
	if len(proofs) == 0 {
		logger.Debug("not eligible for proposal in layer")
		return nil
	}
	logger.With().Info("eligible for proposals in layer", atxID, log.Int("num_proposals", len(proofs)))

	pb.baseBallotProvider.TallyVotes(ctx, layerID)
	// TODO(dshulyak) will get rid from the EncodeVotesWithCurrent option in a followup
	// there are some dependencies in the tests
	opinion, err := pb.baseBallotProvider.EncodeVotes(ctx, tortoise.EncodeVotesWithCurrent(layerID))
	if err != nil {
		return fmt.Errorf("get base ballot: %w", err)
	}

	txList := pb.conState.SelectProposalTXs(layerID, len(proofs))
	p, err := pb.createProposal(ctx, layerID, proofs, atxID, activeSet, beacon, txList, *opinion)
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
	for {
		select {
		case <-pb.ctx.Done():
			return

		case layerID := <-pb.layerTimer:
			lyrCtx := log.WithNewSessionID(ctx)
			if err := pb.handleLayer(lyrCtx, layerID); err != nil {
				pb.logger.WithContext(lyrCtx).With().Warning("failed to create proposal", log.Err(err))
			}
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
