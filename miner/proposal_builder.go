// Package miner is responsible for creating valid blocks that contain valid activation transactions and transactions
package miner

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

const (
	// ATXsPerBallotLimit indicates the maximum number of ATXs a Ballot can reference.
	ATXsPerBallotLimit = 100

	buildDurationErrorThreshold = 10 * time.Second
)

var (
	errGenesis   = errors.New("not building proposals: genesis")
	errNotSynced = errors.New("not building proposals: node not synced")
	errNoBeacon  = errors.New("not building proposals: missing beacon")
)

// ProposalBuilder builds Proposals for a miner.
type ProposalBuilder struct {
	logger      log.Log
	cfg         config
	refBallotDB database.Database

	startOnce  sync.Once
	ctx        context.Context
	cancel     context.CancelFunc
	eg         errgroup.Group
	layerTimer chan types.LayerID

	publisher          pubsub.Publisher
	signer             *signing.EdSigner
	txPool             txPool
	proposalDB         proposalDB
	baseBallotProvider baseBallotProvider
	proposalOracle     proposalOracle
	beaconProvider     system.BeaconGetter
	syncer             system.SyncStateProvider
	projector          projector
}

// config defines configuration for the ProposalBuilder.
type config struct {
	layerSize      uint32
	layersPerEpoch uint32
	dbPath         string
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

// WithDBPath defines the path to create miner's database.
func WithDBPath(path string) Opt {
	return func(pb *ProposalBuilder) {
		pb.cfg.dbPath = path
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

func withRefDatabase(db database.Database) Opt {
	return func(pb *ProposalBuilder) {
		pb.refBallotDB = db
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
	atxDB activationDB,
	publisher pubsub.Publisher,
	pdb proposalDB,
	bbp baseBallotProvider,
	beaconProvider system.BeaconGetter,
	syncer system.SyncStateProvider,
	projector projector,
	txPool txPool,
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
		publisher:          publisher,
		proposalDB:         pdb,
		baseBallotProvider: bbp,
		beaconProvider:     beaconProvider,
		syncer:             syncer,
		projector:          projector,
		txPool:             txPool,
	}

	for _, opt := range opts {
		opt(pb)
	}

	if pb.projector == nil {
		pb.logger.Panic("nil projector")
	}

	if pb.proposalOracle == nil {
		pb.proposalOracle = newMinerOracle(pb.cfg.layerSize, pb.cfg.layersPerEpoch, atxDB, vrfSigner, pb.cfg.minerID, pb.logger)
	}

	if pb.refBallotDB == nil {
		if len(pb.cfg.dbPath) == 0 {
			pb.refBallotDB = database.NewMemDatabase()
		} else {
			var err error
			pb.refBallotDB, err = database.NewLDBDatabase(filepath.Join(pb.cfg.dbPath, "miner"), 16, 16, pb.logger)
			if err != nil {
				pb.logger.With().Panic("cannot create miner database", log.Err(err))
			}
		}
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
	pb.refBallotDB.Close()
}

func getEpochKey(ID types.EpochID) []byte {
	return []byte(fmt.Sprintf("e_%v", ID))
}

func (pb *ProposalBuilder) storeRefBallot(epoch types.EpochID, ballotID types.BallotID) error {
	if err := pb.refBallotDB.Put(getEpochKey(epoch), ballotID.Bytes()); err != nil {
		return fmt.Errorf("put in refDB: %w", err)
	}
	return nil
}

func (pb *ProposalBuilder) getRefBallot(epoch types.EpochID) (types.BallotID, error) {
	var ballotID types.BallotID
	bts, err := pb.refBallotDB.Get(getEpochKey(epoch))
	if err != nil {
		return types.EmptyBallotID, fmt.Errorf("get from refDB: %w", err)
	}
	copy(ballotID[:], bts)
	return ballotID, nil
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
	votes types.Votes,
) (*types.Proposal, error) {
	logger := pb.logger.WithContext(ctx).WithFields(layerID, layerID.GetEpoch())

	if !layerID.After(types.GetEffectiveGenesis()) {
		logger.Panic("attempt to create proposal during genesis")
	}

	ib := &types.InnerBallot{
		AtxID:             atxID,
		EligibilityProofs: proofs,
		Votes:             votes,
		LayerIndex:        layerID,
	}

	epoch := layerID.GetEpoch()
	refBallot, err := pb.getRefBallot(epoch)
	if err != nil {
		if !errors.Is(err, database.ErrNotFound) {
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

	p := &types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: types.Ballot{
				InnerBallot: *ib,
			},
			TxIDs: txIDs,
		},
	}
	p.Ballot.Signature = pb.signer.Sign(p.Ballot.Bytes())
	p.Signature = pb.signer.Sign(p.Bytes())
	if err := p.Initialize(); err != nil {
		logger.Panic("proposal failed to initialize", log.Err(err))
	}
	// NOTE(dshulyak) python tests read this log. alternative is to read number of eligibilities
	// in the python tests, which seems like a bad idea.
	for range p.EligibilityProofs {
		logger.Event().Info("proposal created", log.Inline(p))
	}
	return p, nil
}

func (pb *ProposalBuilder) handleLayer(ctx context.Context, layerID types.LayerID) error {
	var (
		beacon types.Beacon
		err    error
		epoch  = layerID.GetEpoch()
		logger = pb.logger.WithContext(ctx).WithFields(layerID, epoch)
	)

	logger.Info("builder got layer")
	if layerID.GetEpoch().IsGenesis() {
		return errGenesis
	}
	if !pb.syncer.IsSynced(ctx) {
		logger.Info("not synced yet, not building a proposal")
		return errNotSynced
	}
	if beacon, err = pb.beaconProvider.GetBeacon(epoch); err != nil {
		logger.With().Info("beacon not available for epoch", log.Err(err))
		return errNoBeacon
	}

	logger.With().Info("miner got beacon to build proposals", beacon)

	started := time.Now()

	atxID, activeSet, proofs, err := pb.proposalOracle.GetProposalEligibility(layerID, beacon)
	if err != nil {
		if errors.Is(err, errMinerHasNoATXInPreviousEpoch) {
			logger.Info("miner has no ATX in previous epoch. not eligible for proposals")
			events.ReportDoneCreatingProposal(false, layerID.Uint32(), "not eligible to produce proposals")
			return fmt.Errorf("miner no ATX: %w", err)
		}
		events.ReportDoneCreatingProposal(false, layerID.Uint32(), "failed to check for proposal eligibility")
		logger.With().Error("failed to check for proposal eligibility", log.Err(err))
		return fmt.Errorf("proposal eligibility: %w", err)
	}
	if len(proofs) == 0 {
		events.ReportDoneCreatingProposal(false, layerID.Uint32(), "")
		logger.Info("not eligible for proposal in layer")
		return nil
	}

	votes, err := pb.baseBallotProvider.BaseBallot(ctx)
	if err != nil {
		return fmt.Errorf("get base ballot: %w", err)
	}

	logger.With().Info("eligible for one or more proposals in layer", atxID, log.Int("num_proposals", len(proofs)))

	txList, _, err := pb.txPool.SelectTopNTransactions(pb.cfg.txsPerProposal*len(proofs), pb.projector.GetProjection)
	if err != nil {
		events.ReportDoneCreatingProposal(true, layerID.Uint32(), "failed to get txs for proposal")
		logger.With().Error("failed to get txs for proposal", log.Err(err))
		return fmt.Errorf("select TXs: %w", err)
	}
	p, err := pb.createProposal(ctx, layerID, proofs, atxID, activeSet, beacon, txList, *votes)
	if err != nil {
		events.ReportDoneCreatingProposal(true, layerID.Uint32(), "failed to create new proposal")
		logger.With().Error("failed to create new proposal", log.Err(err))
		return err
	}

	pb.saveMetrics(ctx, started, layerID)

	if pb.stopped() {
		return nil
	}

	if p.RefBallot == types.EmptyBallotID {
		ballotID := p.Ballot.ID()
		logger.With().Debug("storing ref ballot", epoch, ballotID)
		if err := pb.storeRefBallot(epoch, ballotID); err != nil {
			logger.With().Error("failed to store ref ballot", ballotID, log.Err(err))
			return err
		}
	}

	pb.eg.Go(func() error {
		// generate a new requestID for the new block message
		newCtx := log.WithNewRequestID(ctx, layerID, p.ID())
		// validation handler, where proposal is persisted, is applied synchronously before
		// proposal is sent over the network
		data, err := codec.Encode(p)
		if err != nil {
			logger.With().Panic("failed to serialize proposal", log.Err(err))
		}
		if err = pb.publisher.Publish(newCtx, proposals.NewProposalProtocol, data); err != nil {
			logger.WithContext(newCtx).With().Error("failed to send proposal", log.Err(err))
		}
		events.ReportDoneCreatingProposal(true, layerID.Uint32(), "")
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
