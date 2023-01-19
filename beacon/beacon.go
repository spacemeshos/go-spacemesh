package beacon

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ALTree/bigfloat"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spacemeshos/fixed"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/beacon/metrics"
	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	proposalPrefix  = "BP"
	numEpochsToKeep = 3
)

var (
	errBeaconNotCalculated = errors.New("beacon is not calculated for this epoch")
	errZeroEpochWeight     = errors.New("zero epoch weight provided")
	errDifferentBeacon     = errors.New("different beacons detected")
	errGenesis             = errors.New("genesis")
	errNodeNotSynced       = errors.New("nodes not synced")
	errProtocolRunning     = errors.New("last beacon protocol still running")
)

type (
	proposals    = struct{ valid, potentiallyValid proposalSet }
	allVotes     = struct{ support, against proposalSet }
	beaconWeight = struct {
		ballots     map[types.BallotID]struct{}
		totalWeight fixed.Fixed
		weightUnits int
	}
)

// Opt for configuring beacon protocol.
type Opt func(*ProtocolDriver)

// WithLogger defines logger for beacon.
func WithLogger(logger log.Log) Opt {
	return func(pd *ProtocolDriver) {
		pd.logger = logger
	}
}

// WithContext defines context for beacon.
func WithContext(ctx context.Context) Opt {
	return func(pd *ProtocolDriver) {
		pd.ctx = ctx
	}
}

// WithConfig defines protocol parameters.
func WithConfig(cfg Config) Opt {
	return func(pd *ProtocolDriver) {
		pd.config = cfg
	}
}

func withWeakCoin(wc coin) Opt {
	return func(pd *ProtocolDriver) {
		pd.weakCoin = wc
	}
}

// New returns a new ProtocolDriver.
func New(
	nodeID types.NodeID,
	publisher pubsub.Publisher,
	edSigner signer,
	pubKeyExtractor pubKeyExtractor,
	vrfSigner vrfSigner,
	vrfVerifier vrfVerifier,
	nonceFetcher nonceFetcher,
	cdb *datastore.CachedDB,
	clock layerClock,
	opts ...Opt,
) *ProtocolDriver {
	pd := &ProtocolDriver{
		ctx:             context.Background(),
		logger:          log.NewNop(),
		config:          DefaultConfig(),
		nodeID:          nodeID,
		publisher:       publisher,
		edSigner:        edSigner,
		pubKeyExtractor: pubKeyExtractor,
		vrfSigner:       vrfSigner,
		vrfVerifier:     vrfVerifier,
		nonceFetcher:    nonceFetcher,
		cdb:             cdb,
		clock:           clock,
		beacons:         make(map[types.EpochID]types.Beacon),
		ballotsBeacons:  make(map[types.EpochID]map[types.Beacon]*beaconWeight),
		states:          make(map[types.EpochID]*state),
	}
	for _, opt := range opts {
		opt(pd)
	}

	pd.ctx, pd.cancel = context.WithCancel(pd.ctx)
	pd.theta = new(big.Float).SetRat(pd.config.Theta)
	if pd.weakCoin == nil {
		pd.weakCoin = weakcoin.New(pd.publisher, cdb, nodeID, vrfSigner, vrfVerifier, nonceFetcher,
			weakcoin.WithLog(pd.logger.WithName("weakCoin")),
			weakcoin.WithMaxRound(pd.config.RoundsNumber),
		)
	}

	pd.metricsCollector = metrics.NewBeaconMetricsCollector(pd.gatherMetricsData, pd.logger.WithName("metrics"))
	return pd
}

// ProtocolDriver is the driver for the beacon protocol.
type ProtocolDriver struct {
	inProtocol uint64
	logger     log.Log
	eg         errgroup.Group
	ctx        context.Context
	cancel     context.CancelFunc
	startOnce  sync.Once

	config          Config
	nodeID          types.NodeID
	sync            system.SyncStateProvider
	publisher       pubsub.Publisher
	edSigner        signer
	pubKeyExtractor pubKeyExtractor
	vrfSigner       vrfSigner
	vrfVerifier     vrfVerifier
	nonceFetcher    nonceFetcher
	weakCoin        coin
	theta           *big.Float

	clock       layerClock
	layerTicker chan types.LayerID
	cdb         *datastore.CachedDB

	mu sync.RWMutex

	// these fields are separate from state because we don't want to pre-maturely create a state
	// for proposals in epoch currentEpoch+1, because we may not have all ATXs for currentEpoch yet.
	roundInProgress                        types.RoundID
	earliestProposalTime, earliestVoteTime time.Time
	// states for the current epoch and the next epoch. we accept early proposals for the next epoch.
	// state for an epoch is created on demand:
	// - when the first proposal of the epoch comes in
	// - when the protocol starts running for the epoch
	// whichever happens first.
	// we start accepting early proposals for the next epoch pd.config.GracePeriodDuration before
	// the next epoch begins.
	states map[types.EpochID]*state

	// beacons store calculated beacons as the result of the beacon protocol.
	// the map key is the target epoch when beacon is used. if a beacon is calculated in epoch N, it will be used
	// in epoch N+1.
	beacons map[types.EpochID]types.Beacon
	// ballotsBeacons store beacons collected from ballots.
	// the map key is the epoch when the ballot is published. the beacon value is calculated in the
	// previous epoch and used in the current epoch.
	ballotsBeacons map[types.EpochID]map[types.Beacon]*beaconWeight

	// metrics
	metricsCollector *metrics.BeaconMetricsCollector
	metricsRegistry  *prometheus.Registry
}

// SetSyncState updates sync state provider. Must be executed only once.
func (pd *ProtocolDriver) SetSyncState(sync system.SyncStateProvider) {
	if pd.sync != nil {
		pd.logger.Panic("sync state provider can be updated only once")
	}
	pd.sync = sync
}

// for testing.
func (pd *ProtocolDriver) setMetricsRegistry(registry *prometheus.Registry) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.metricsRegistry = registry
}

// Start starts listening for layers and outputs.
func (pd *ProtocolDriver) Start(ctx context.Context) {
	pd.startOnce.Do(func() {
		pd.logger.With().Info("starting beacon protocol", log.String("config", fmt.Sprintf("%+v", pd.config)))
		if pd.sync == nil {
			pd.logger.Panic("update sync state provider can't be nil")
		}

		pd.layerTicker = pd.clock.Subscribe()
		pd.metricsCollector.Start(nil)

		pd.setProposalTimeForNextEpoch()
		pd.eg.Go(func() error {
			pd.listenLayers(ctx)
			return nil
		})
	})
}

// Close closes ProtocolDriver.
func (pd *ProtocolDriver) Close() {
	pd.logger.Info("closing beacon protocol")
	pd.clock.Unsubscribe(pd.layerTicker)
	pd.metricsCollector.Stop()
	pd.cancel()
	pd.logger.Info("waiting for beacon goroutines to finish")
	if err := pd.eg.Wait(); err != nil {
		pd.logger.With().Info("received error waiting for goroutines to finish", log.Err(err))
	}
	pd.logger.Info("beacon goroutines finished")
}

// isClosed returns true if the beacon protocol is shutting down.
func (pd *ProtocolDriver) isClosed() bool {
	select {
	case <-pd.ctx.Done():
		return true
	default:
		return false
	}
}

// ReportBeaconFromBallot reports the beacon value in a ballot along with the smesher's weight unit.
func (pd *ProtocolDriver) ReportBeaconFromBallot(epoch types.EpochID, ballot *types.Ballot, beacon types.Beacon, weightPer fixed.Fixed) {
	pd.recordBeacon(epoch, ballot, beacon, weightPer)

	if _, err := pd.GetBeacon(epoch); err == nil {
		// already has beacon. i.e. we had participated in the beacon protocol during the last epoch
		return
	}

	if eBeacon := pd.findMajorityBeacon(epoch); eBeacon != types.EmptyBeacon {
		if err := pd.setBeacon(epoch, eBeacon); err != nil {
			pd.logger.With().Error("beacon sync: failed to set beacon", log.Err(err))
		}
	}
}

func (pd *ProtocolDriver) recordBeacon(epochID types.EpochID, ballot *types.Ballot, beacon types.Beacon, weightPer fixed.Fixed) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	// using ballot weight here because we're sampling miners for the beacon value recorded
	// in the ballots. we can't just take the entire ATX weight because otherwise, a "whale"
	// ATX would dominate the voting.
	ballotWeight := weightPer.Mul(fixed.New(len(ballot.EligibilityProofs)))
	weightUnit := len(ballot.EligibilityProofs)
	if _, ok := pd.ballotsBeacons[epochID]; !ok {
		pd.ballotsBeacons[epochID] = make(map[types.Beacon]*beaconWeight)
	}
	entry, ok := pd.ballotsBeacons[epochID][beacon]
	if !ok {
		pd.ballotsBeacons[epochID][beacon] = &beaconWeight{
			ballots:     map[types.BallotID]struct{}{ballot.ID(): {}},
			totalWeight: ballotWeight,
			weightUnits: weightUnit,
		}
		pd.logger.With().Debug("added beacon from ballot",
			epochID,
			ballot.ID(),
			beacon,
			log.Stringer("weight_per", weightPer),
			log.Int("weight_units", weightUnit),
			log.Stringer("weight", ballotWeight))
		return
	}

	// checks if we have recorded this ballot before
	if _, ok = entry.ballots[ballot.ID()]; ok {
		pd.logger.With().Warning("ballot already reported beacon", epochID, ballot.ID())
		return
	}

	entry.ballots[ballot.ID()] = struct{}{}
	entry.totalWeight = entry.totalWeight.Add(ballotWeight)
	entry.weightUnits += weightUnit
	pd.logger.With().Debug("added beacon from ballot",
		epochID,
		ballot.ID(),
		beacon,
		log.Stringer("weight_per", weightPer),
		log.Int("weight_units", weightUnit),
		log.Stringer("weight", ballotWeight))
}

func (pd *ProtocolDriver) findMajorityBeacon(epoch types.EpochID) types.Beacon {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	epochBeacons, ok := pd.ballotsBeacons[epoch]
	if !ok {
		return types.EmptyBeacon
	}
	totalWeight := fixed.New64(0)
	totalWeightUnits := 0
	for _, bw := range epochBeacons {
		totalWeightUnits += bw.weightUnits
		totalWeight = totalWeight.Add(bw.totalWeight)
	}

	logger := pd.logger.WithFields(epoch, log.Int("total_weight_units", totalWeightUnits))
	if totalWeightUnits < pd.config.BeaconSyncWeightUnits {
		logger.Debug("not enough weight units to determine beacon")
		return types.EmptyBeacon
	}

	majorityWeight := totalWeight.Div(fixed.New(2))
	maxWeight := fixed.New64(0)
	var bPlurality types.Beacon
	for beacon, bw := range epochBeacons {
		if bw.totalWeight.GreaterThan(majorityWeight) {
			logger.With().Info("beacon determined for epoch",
				beacon,
				log.Int("total_weight_unit", totalWeightUnits),
				log.Stringer("total_weight", totalWeight),
				log.Stringer("beacon_weight", bw.totalWeight))
			return beacon
		}
		if bw.totalWeight.GreaterThan(maxWeight) {
			bPlurality = beacon
			maxWeight = bw.totalWeight
		}
	}
	logger.With().Warning("beacon determined for epoch by plurality, not majority",
		bPlurality,
		log.Int("total_weight_unit", totalWeightUnits),
		log.Stringer("total_weight", totalWeight),
		log.Stringer("beacon_weight", maxWeight))
	return bPlurality
}

// GetBeacon returns the beacon for the specified epoch or an error if it doesn't exist.
func (pd *ProtocolDriver) GetBeacon(targetEpoch types.EpochID) (types.Beacon, error) {
	// Returns empty beacon up to epoch 2:
	// epoch 0 - no ATXs
	// epoch 1 - ATXs, no beacon protocol, only genesis ballot needs to set the beacon value
	// epoch 2 - ATXs, proposals/blocks, beacon protocol (but targeting for epoch 3)
	if targetEpoch <= types.EpochID(2) {
		return types.HexToBeacon(types.BootstrapBeacon), nil
	}

	beacon := pd.getBeacon(targetEpoch)
	if beacon != types.EmptyBeacon {
		return beacon, nil
	}

	beacon, err := pd.getPersistedBeacon(targetEpoch)
	if err == nil {
		return beacon, nil
	}

	if errors.Is(err, sql.ErrNotFound) {
		pd.logger.With().Warning("beacon not available", targetEpoch)
		return types.EmptyBeacon, errBeaconNotCalculated
	}
	pd.logger.With().Error("failed to get beacon from db", targetEpoch, log.Err(err))
	return types.EmptyBeacon, fmt.Errorf("get beacon from DB: %w", err)
}

func (pd *ProtocolDriver) getBeacon(epoch types.EpochID) types.Beacon {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	if beacon, ok := pd.beacons[epoch]; ok {
		return beacon
	}
	return types.EmptyBeacon
}

func (pd *ProtocolDriver) setBeacon(targetEpoch types.EpochID, beacon types.Beacon) error {
	if err := pd.persistBeacon(targetEpoch, beacon); err != nil {
		return err
	}
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, ok := pd.beacons[targetEpoch]; !ok {
		pd.beacons[targetEpoch] = beacon
	}
	return nil
}

func (pd *ProtocolDriver) persistBeacon(epoch types.EpochID, beacon types.Beacon) error {
	if err := beacons.Add(pd.cdb, epoch, beacon); err != nil {
		if !errors.Is(err, sql.ErrObjectExists) {
			pd.logger.With().Error("failed to persist beacon", epoch, beacon, log.Err(err))
			return fmt.Errorf("persist beacon: %w", err)
		}
		// when syncing, multiple ballots come in concurrently and may cause beacon to be determined
		// and persisted at the same time.
		savedBeacon := pd.getBeacon(epoch)
		if savedBeacon != beacon {
			pd.logger.With().Error("trying to persist different beacon", epoch, beacon, log.String("saved_beacon", savedBeacon.ShortString()))
			return errDifferentBeacon
		}
		pd.logger.With().Warning("beacon already exists for epoch", epoch, beacon)
	}
	return nil
}

func (pd *ProtocolDriver) getPersistedBeacon(epoch types.EpochID) (types.Beacon, error) {
	data, err := beacons.Get(pd.cdb, epoch)
	if err != nil {
		return types.EmptyBeacon, fmt.Errorf("get from DB: %w", err)
	}
	return data, nil
}

func (pd *ProtocolDriver) setBeginProtocol(ctx context.Context) {
	if !atomic.CompareAndSwapUint64(&pd.inProtocol, 0, 1) {
		pd.logger.WithContext(ctx).Error("attempt to begin the beacon protocol more than once")
	}
}

func (pd *ProtocolDriver) setEndProtocol(ctx context.Context) {
	if !atomic.CompareAndSwapUint64(&pd.inProtocol, 1, 0) {
		pd.logger.WithContext(ctx).Error("attempt to end the beacon protocol more than once")
	}
}

func (pd *ProtocolDriver) isInProtocol() bool {
	return atomic.LoadUint64(&pd.inProtocol) == 1
}

func (pd *ProtocolDriver) initEpochStateIfNotPresent(logger log.Log, epoch types.EpochID) (*state, error) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if s, ok := pd.states[epoch]; ok {
		return s, nil
	}

	epochWeight, atxids, err := pd.cdb.GetEpochWeight(epoch)
	if err != nil {
		logger.With().Error("failed to get weight targeting epoch", log.Err(err))
		return nil, fmt.Errorf("get epoch weight: %w", err)
	}
	if epochWeight == 0 {
		logger.With().Error("zero weight targeting epoch", log.Err(errZeroEpochWeight))
		return nil, errZeroEpochWeight
	}

	pd.states[epoch] = newState(logger, pd.config, epochWeight, atxids)
	return pd.states[epoch], nil
}

func (pd *ProtocolDriver) setProposalTimeForNextEpoch() {
	epoch := pd.currentEpoch()
	nextEpochStart := pd.clock.LayerToTime((epoch + 1).FirstLayer())
	t := nextEpochStart.Add(-pd.config.GracePeriodDuration)
	pd.logger.With().Debug("earliest proposal time for epoch",
		epoch+1,
		log.Time("earliest_time", t))

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.earliestProposalTime = t
}

func (pd *ProtocolDriver) setupEpoch(logger log.Log, epoch types.EpochID) ([]types.ATXID, error) {
	s, err := pd.initEpochStateIfNotPresent(logger, epoch)
	if err != nil {
		return nil, err
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.roundInProgress = types.FirstRound
	pd.earliestVoteTime = time.Time{}

	return s.atxs, nil
}

func (pd *ProtocolDriver) cleanupEpoch(epoch types.EpochID) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	for e := range pd.states {
		if e < epoch {
			delete(pd.states, e)
		}
	}
	delete(pd.states, epoch)

	if epoch <= numEpochsToKeep {
		return
	}
	oldest := epoch - numEpochsToKeep
	delete(pd.beacons, oldest)
	delete(pd.ballotsBeacons, oldest)
}

// listens to new layers.
func (pd *ProtocolDriver) listenLayers(ctx context.Context) {
	pd.logger.With().Info("starting listening layers")

	for {
		select {
		case <-pd.ctx.Done():
			return
		case layer := <-pd.layerTicker:
			pd.logger.With().Debug("received tick", layer)
			if layer.FirstInEpoch() {
				epoch := layer.GetEpoch()
				pd.setProposalTimeForNextEpoch()
				pd.logger.With().Info("first layer in epoch", layer, epoch)
				pd.eg.Go(func() error {
					_ = pd.onNewEpoch(ctx, epoch)
					return nil
				})
			}
		}
	}
}

func (pd *ProtocolDriver) setRoundInProgress(round types.RoundID) {
	now := time.Now()
	var nextRoundStartTime time.Time
	if round == types.FirstRound {
		nextRoundStartTime = now.Add(pd.config.FirstVotingRoundDuration)
	} else {
		nextRoundStartTime = now.Add(pd.config.VotingRoundDuration + pd.config.WeakCoinRoundDuration)
	}
	earliestVoteTime := nextRoundStartTime.Add(-pd.config.GracePeriodDuration)
	pd.logger.With().Debug("earliest vote time for next round",
		round,
		log.Uint32("next_round", uint32(round+1)),
		log.Time("earliest_time", earliestVoteTime))

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.roundInProgress = round
	pd.earliestVoteTime = earliestVoteTime
}

func (pd *ProtocolDriver) onNewEpoch(ctx context.Context, epoch types.EpochID) error {
	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	defer pd.cleanupEpoch(epoch)

	if epoch.IsGenesis() {
		logger.Info("not running beacon protocol: genesis epochs")
		return errGenesis
	}
	if !pd.sync.IsSynced(ctx) {
		logger.Info("not running beacon protocol: node not synced")
		return errNodeNotSynced
	}
	if pd.isInProtocol() {
		logger.Error("not running beacon protocol: last one still running")
		return errProtocolRunning
	}

	// make sure this node has ATX in the last epoch and is eligible to participate in the beacon protocol
	atxID, err := atxs.GetIDByEpochAndNodeID(pd.cdb, epoch-1, pd.nodeID)
	if err != nil {
		logger.With().Info("not running beacon protocol: no own ATX in last epoch", log.Err(err))
		return err
	}

	atxids, err := pd.setupEpoch(logger, epoch)
	if err != nil {
		logger.With().Error("epoch not setup correctly", log.Err(err))
		return err
	}

	logger.With().Info("participating beacon protocol with ATX", atxID)
	pd.runProtocol(ctx, epoch, atxids)
	return nil
}

func (pd *ProtocolDriver) runProtocol(ctx context.Context, epoch types.EpochID, atxs []types.ATXID) {
	ctx = log.WithNewSessionID(ctx)
	targetEpoch := epoch + 1
	logger := pd.logger.WithContext(ctx).WithFields(epoch, log.Uint32("target_epoch", uint32(targetEpoch)))

	pd.setBeginProtocol(ctx)
	defer pd.setEndProtocol(ctx)

	pd.startWeakCoinEpoch(ctx, epoch, atxs)
	defer pd.weakCoin.FinishEpoch(ctx, epoch)

	if err := pd.runProposalPhase(ctx, epoch); err != nil {
		logger.With().Warning("proposal phase failed", log.Err(err))
		return
	}
	lastRoundOwnVotes, err := pd.runConsensusPhase(ctx, epoch)
	if err != nil {
		logger.With().Warning("consensus phase failed", log.Err(err))
		return
	}

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	beacon := calcBeacon(logger, lastRoundOwnVotes)

	if err := pd.setBeacon(targetEpoch, beacon); err != nil {
		logger.With().Error("failed to set beacon", log.Err(err))
		return
	}

	logger.With().Info("beacon set for epoch", beacon)
}

func calcBeacon(logger log.Log, lastRoundVotes allVotes) types.Beacon {
	logger.Info("calculating beacon")

	allHashes := lastRoundVotes.support.sort()
	allHashHexes := make([]string, len(allHashes))
	for i, h := range allHashes {
		allHashHexes[i] = hex.EncodeToString(h)
	}
	logger.With().Debug("calculating beacon from this hash list",
		log.String("hashes", strings.Join(allHashHexes, ", ")))

	// Beacon should appear to have the same entropy as the initial proposals, hence cropping it
	// to the same size as the proposal
	beacon := types.BytesToBeacon(allHashes.hash().Bytes())
	logger.With().Info("calculated beacon", beacon, log.Int("num_hashes", len(allHashes)))

	return beacon
}

func (pd *ProtocolDriver) runProposalPhase(ctx context.Context, epoch types.EpochID) error {
	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	logger.Info("starting beacon proposal phase")

	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, pd.config.ProposalDuration)
	defer cancel()

	pd.eg.Go(func() error {
		pd.sendProposal(ctx, epoch)
		return nil
	})

	select {
	case <-ctx.Done():
	case <-pd.ctx.Done():
		return pd.ctx.Err()
	}

	if err := pd.markProposalPhaseFinished(epoch, time.Now()); err != nil {
		return err
	}

	logger.Debug("beacon proposal phase finished")
	return nil
}

func (pd *ProtocolDriver) sendProposal(ctx context.Context, epoch types.EpochID) {
	if pd.isClosed() {
		return
	}

	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	nonce, err := pd.nonceFetcher.VRFNonce(pd.nodeID, epoch)
	if err != nil {
		logger.With().Panic("failed to get VRF nonce", log.Err(err))
	}
	proposedSignature := buildSignedProposal(ctx, pd.vrfSigner, epoch, nonce, pd.logger)
	if !pd.checkProposalEligibility(logger, epoch, proposedSignature) {
		logger.With().Debug("own proposal doesn't pass threshold",
			log.String("proposal", string(proposedSignature)),
		)
		// proposal is not sent
		return
	}

	logger.With().Debug("own proposal passes threshold",
		log.String("proposal", string(proposedSignature)),
	)

	// concat them into a single proposal message
	m := ProposalMessage{
		EpochID:      epoch,
		NodeID:       pd.nodeID,
		VRFSignature: proposedSignature,
	}

	serialized, err := codec.Encode(&m)
	if err != nil {
		logger.With().Panic("failed to serialize message for gossip", log.Err(err))
	}

	pd.sendToGossip(ctx, pubsub.BeaconProposalProtocol, serialized)
	logger.With().Info("beacon proposal sent", log.String("message", m.String()))
}

// runConsensusPhase runs K voting rounds and returns result from last weak coin round.
func (pd *ProtocolDriver) runConsensusPhase(ctx context.Context, epoch types.EpochID) (allVotes, error) {
	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	logger.Info("starting consensus phase")

	// For K rounds: In each round that lasts δ, wait for votes to come in.
	// For next rounds,
	// wait for δ time, and construct a message that points to all messages from previous round received by δ.
	// rounds 1 to K
	timer := time.NewTimer(pd.config.FirstVotingRoundDuration)
	defer timer.Stop()

	var (
		ownVotes  allVotes
		undecided []string
		err       error
	)
	for round := types.FirstRound; round < pd.config.RoundsNumber; round++ {
		round := round
		pd.setRoundInProgress(round)
		rLogger := logger.WithFields(round)
		votes := ownVotes
		pd.eg.Go(func() error {
			if round == types.FirstRound {
				if err := pd.sendFirstRoundVote(ctx, epoch); err != nil {
					rLogger.With().Error("failed to send proposal vote", log.Err(err))
				}
			} else {
				if err := pd.sendFollowingVote(ctx, epoch, round, votes); err != nil {
					rLogger.With().Error("failed to send following vote", log.Err(err))
				}
			}
			return nil
		})

		select {
		case <-timer.C:
		case <-ctx.Done():
			return allVotes{}, fmt.Errorf("context done: %w", ctx.Err())
		}

		// note that votes after this calcVotes() call will _not_ be counted towards our votes
		// for this round, as the late votes can be cast after the weak coin is revealed. we
		// count them towards our votes in the next round.
		ownVotes, undecided, err = pd.calcVotesBeforeWeakCoin(rLogger, epoch)
		if err != nil {
			return allVotes{}, err
		}
		if round != types.FirstRound {
			timer.Reset(pd.config.WeakCoinRoundDuration)

			pd.eg.Go(func() error {
				if err := pd.weakCoin.StartRound(ctx, round); err != nil {
					rLogger.With().Error("failed to publish weak coin proposal", log.Err(err))
				}
				return nil
			})
			select {
			case <-timer.C:
			case <-ctx.Done():
				return allVotes{}, fmt.Errorf("context done: %w", ctx.Err())
			}
			pd.weakCoin.FinishRound(ctx)
			tallyUndecided(&ownVotes, undecided, pd.weakCoin.Get(ctx, epoch, round))
		}
		timer.Reset(pd.config.VotingRoundDuration)
	}

	logger.Info("consensus phase finished")
	return ownVotes, nil
}

func (pd *ProtocolDriver) startWeakCoinEpoch(ctx context.Context, epoch types.EpochID, atxs []types.ATXID) {
	// we need to pass a map with spacetime unit allowances before any round is started
	ua := weakcoin.UnitAllowances{}
	for _, id := range atxs {
		header, err := pd.cdb.GetAtxHeader(id)
		if err != nil {
			pd.logger.WithContext(ctx).With().Panic("unable to load atx header", log.Err(err))
		}
		ua[string(header.NodeID.Bytes())] += uint64(header.NumUnits)
	}

	pd.weakCoin.StartEpoch(ctx, epoch, ua)
}

func (pd *ProtocolDriver) markProposalPhaseFinished(epoch types.EpochID, finishedAt time.Time) error {
	pd.logger.With().Debug("proposal phase finished", epoch, log.Time("finished_at", finishedAt))
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, ok := pd.states[epoch]; !ok {
		return errEpochNotActive
	}
	pd.states[epoch].proposalPhaseFinishedTime = finishedAt
	return nil
}

func (pd *ProtocolDriver) calcVotesBeforeWeakCoin(logger log.Log, epoch types.EpochID) (allVotes, []string, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	if _, ok := pd.states[epoch]; !ok {
		return allVotes{}, nil, errEpochNotActive
	}
	decided, undecided := calcVotes(logger, pd.theta, pd.states[epoch])
	return decided, undecided, nil
}

func (pd *ProtocolDriver) genFirstRoundMsgBody(epoch types.EpochID) (FirstVotingMessageBody, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if _, ok := pd.states[epoch]; !ok {
		return FirstVotingMessageBody{}, errEpochNotActive
	}
	s := pd.states[epoch]
	return FirstVotingMessageBody{
		EpochID:                   epoch,
		ValidProposals:            s.incomingProposals.valid.sort(),
		PotentiallyValidProposals: s.incomingProposals.potentiallyValid.sort(),
	}, nil
}

func (pd *ProtocolDriver) sendFirstRoundVote(ctx context.Context, epoch types.EpochID) error {
	mb, err := pd.genFirstRoundMsgBody(epoch)
	if err != nil {
		return err
	}

	encoded, err := codec.Encode(&mb)
	if err != nil {
		pd.logger.With().Panic("failed to serialize message for signing", log.Err(err))
	}
	sig := pd.edSigner.Sign(encoded)

	m := FirstVotingMessage{
		FirstVotingMessageBody: mb,
		Signature:              sig,
	}

	pd.logger.WithContext(ctx).With().Debug("sending first round vote",
		epoch,
		types.FirstRound,
		log.String("message", m.String()))

	serialized, err := codec.Encode(&m)
	if err != nil {
		pd.logger.With().Panic("failed to serialize message for gossip", log.Err(err))
	}

	pd.sendToGossip(ctx, pubsub.BeaconFirstVotesProtocol, serialized)
	return nil
}

func (pd *ProtocolDriver) getFirstRoundVote(epoch types.EpochID, minerPK *signing.PublicKey) ([][]byte, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	if _, ok := pd.states[epoch]; !ok {
		return nil, errEpochNotActive
	}
	return pd.states[epoch].getMinerFirstRoundVote(minerPK)
}

func (pd *ProtocolDriver) sendFollowingVote(ctx context.Context, epoch types.EpochID, round types.RoundID, ownCurrentRoundVotes allVotes) error {
	firstRoundVotes, err := pd.getFirstRoundVote(epoch, pd.edSigner.PublicKey())
	if err != nil {
		return fmt.Errorf("get own first round votes %v: %w", pd.edSigner.PublicKey().String(), err)
	}

	bitVector := encodeVotes(ownCurrentRoundVotes, firstRoundVotes)
	mb := FollowingVotingMessageBody{
		EpochID:        epoch,
		RoundID:        round,
		VotesBitVector: bitVector,
	}

	encoded, err := codec.Encode(&mb)
	if err != nil {
		pd.logger.With().Panic("failed to serialize message for signing", log.Err(err))
	}
	sig := pd.edSigner.Sign(encoded)

	m := FollowingVotingMessage{
		FollowingVotingMessageBody: mb,
		Signature:                  sig,
	}

	pd.logger.WithContext(ctx).With().Debug("sending following round vote",
		epoch,
		round,
		log.String("message", m.String()))

	serialized, err := codec.Encode(&m)
	if err != nil {
		pd.logger.With().Panic("failed to serialize message for gossip", log.Err(err))
	}

	pd.sendToGossip(ctx, pubsub.BeaconFollowingVotesProtocol, serialized)
	return nil
}

type proposalChecker struct {
	threshold *big.Int
}

func createProposalChecker(logger log.Log, conf Config, numATXs int) eligibilityChecker {
	if numATXs == 0 {
		logger.Error("creating proposal checker with zero atxs")
		return &proposalChecker{threshold: big.NewInt(0)}
	}

	threshold := atxThreshold(conf.Kappa, conf.Q, numATXs)
	logger.With().Info("created proposal checker with ATX threshold",
		log.Int("num_atxs", numATXs),
		log.String("threshold", threshold.String()))
	return &proposalChecker{threshold: threshold}
}

func (pc *proposalChecker) IsProposalEligible(proposal []byte) bool {
	proposalInt := new(big.Int).SetBytes(proposal)
	return proposalInt.Cmp(pc.threshold) == -1
}

// TODO(nkryuchkov): Consider replacing github.com/ALTree/bigfloat.
func atxThresholdFraction(kappa int, q *big.Rat, numATXs int) *big.Float {
	if numATXs == 0 {
		return big.NewFloat(0)
	}

	// threshold(k, q, W) = 1 - (2 ^ (- (k/((1-q)*W))))
	// Floating point: 1 - math.Pow(2.0, -(float64(tb.config.Kappa)/((1.0-tb.config.Q)*float64(numATXs))))
	// Fixed point:
	v := new(big.Float).Sub(
		new(big.Float).SetInt64(1),
		bigfloat.Pow(
			new(big.Float).SetInt64(2),
			new(big.Float).SetRat(
				new(big.Rat).Neg(
					new(big.Rat).Quo(
						new(big.Rat).SetInt(big.NewInt(int64(kappa))),
						new(big.Rat).Mul(
							new(big.Rat).Sub(
								new(big.Rat).SetInt64(1.0),
								q,
							),
							new(big.Rat).SetInt(big.NewInt(int64(numATXs))),
						),
					),
				),
			),
		),
	)

	return v
}

// TODO(nkryuchkov): Consider having a generic function for probabilities.
func atxThreshold(kappa int, q *big.Rat, numATXs int) *big.Int {
	const (
		sigLengthBytes = 80
		sigLengthBits  = sigLengthBytes * 8
	)

	fraction := atxThresholdFraction(kappa, q, numATXs)
	two := big.NewInt(2)
	signatureLengthBigInt := big.NewInt(sigLengthBits)

	maxPossibleNumberBigInt := new(big.Int).Exp(two, signatureLengthBigInt, nil)
	maxPossibleNumberBigFloat := new(big.Float).SetInt(maxPossibleNumberBigInt)

	thresholdBigFloat := new(big.Float).Mul(maxPossibleNumberBigFloat, fraction)
	threshold, _ := thresholdBigFloat.Int(nil)

	return threshold
}

func buildSignedProposal(ctx context.Context, signer vrfSigner, epoch types.EpochID, nonce types.VRFPostIndex, logger log.Log) []byte {
	p := buildProposal(epoch, nonce, logger)
	signature := signer.Sign(p)
	logger.WithContext(ctx).With().Debug("calculated signature",
		epoch,
		log.String("proposal", hex.EncodeToString(p)),
		log.String("signature", hex.EncodeToString(signature)),
	)

	return signature
}

// ProposalVrfMessage is a message for buildProposal below.
type ProposalVrfMessage struct {
	Type  types.EligibilityType
	Nonce types.VRFPostIndex
	Epoch uint32
}

func buildProposal(epoch types.EpochID, nonce types.VRFPostIndex, logger log.Log) []byte {
	message := &ProposalVrfMessage{
		Type:  types.EligibilityBeacon,
		Nonce: nonce,
		Epoch: uint32(epoch),
	}

	b, err := codec.Encode(message)
	if err != nil {
		logger.With().Panic("failed to serialize proposal", log.Err(err))
	}
	return b
}

func (pd *ProtocolDriver) sendToGossip(ctx context.Context, protocol string, serialized []byte) {
	// NOTE(dshulyak) moved to goroutine because self-broadcast is applied synchronously
	pd.eg.Go(func() error {
		if err := pd.publisher.Publish(ctx, protocol, serialized); err != nil {
			pd.logger.With().Error("failed to broadcast",
				log.String("protocol", protocol),
				log.Err(err),
			)
		}
		return nil
	})
}

func (pd *ProtocolDriver) gatherMetricsData() ([]*metrics.BeaconStats, *metrics.BeaconStats) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	epoch := pd.clock.GetCurrentLayer().GetEpoch()
	var observed []*metrics.BeaconStats
	if epochBeacons, ok := pd.ballotsBeacons[epoch]; ok {
		for beacon, stats := range epochBeacons {
			stat := &metrics.BeaconStats{
				Epoch:      epoch,
				Beacon:     beacon.ShortString(),
				Weight:     stats.totalWeight,
				WeightUnit: stats.weightUnits,
			}
			observed = append(observed, stat)
		}
	}
	var calculated *metrics.BeaconStats
	// whether the beacon for the next epoch is calculated
	if beacon, ok := pd.beacons[epoch+1]; ok {
		calculated = &metrics.BeaconStats{
			Epoch:  epoch + 1,
			Beacon: beacon.ShortString(),
		}
	}

	return observed, calculated
}
