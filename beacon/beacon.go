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
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	numEpochsToKeep = 3
)

var (
	errBeaconNotCalculated = errors.New("beacon is not calculated for this epoch")
	errZeroEpochWeight     = errors.New("zero epoch weight provided")
	errDifferentBeacon     = errors.New("different beacons detected")
	errGenesis             = errors.New("genesis")
	errNodeNotSynced       = errors.New("nodes not synced")
	errProtocolRunning     = errors.New("last beacon protocol still running")
	errNoProposals         = errors.New("no proposals")
)

type (
	proposals    = struct{ valid, potentiallyValid proposalSet }
	allVotes     = struct{ support, against proposalSet }
	beaconWeight = struct {
		ballots        map[types.BallotID]struct{}
		totalWeight    fixed.Fixed
		numEligibility int
	}
)

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

func withNonceFetcher(nf nonceFetcher) Opt {
	return func(pd *ProtocolDriver) {
		pd.nonceFetcher = nf
	}
}

// New returns a new ProtocolDriver.
func New(
	nodeID types.NodeID,
	publisher pubsub.Publisher,
	edSigner *signing.EdSigner,
	edVerifier *signing.EdVerifier,
	vrfSigner vrfSigner,
	vrfVerifier vrfVerifier,
	cdb *datastore.CachedDB,
	clock layerClock,
	opts ...Opt,
) *ProtocolDriver {
	pd := &ProtocolDriver{
		ctx:            context.Background(),
		logger:         log.NewNop(),
		config:         DefaultConfig(),
		nodeID:         nodeID,
		publisher:      publisher,
		edSigner:       edSigner,
		edVerifier:     edVerifier,
		vrfSigner:      vrfSigner,
		vrfVerifier:    vrfVerifier,
		cdb:            cdb,
		clock:          clock,
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: make(map[types.EpochID]map[types.Beacon]*beaconWeight),
		states:         make(map[types.EpochID]*state),
		results:        make(chan result.Beacon, 100),
	}
	for _, opt := range opts {
		opt(pd)
	}
	pd.msgTimes = &messageTimes{
		clock: clock,
		conf:  pd.config,
	}

	pd.ctx, pd.cancel = context.WithCancel(pd.ctx)
	pd.theta = new(big.Float).SetRat(pd.config.Theta)
	if pd.nonceFetcher == nil {
		pd.nonceFetcher = defaultFetcher{cdb: cdb}
	}

	if pd.weakCoin == nil {
		pd.weakCoin = weakcoin.New(pd.publisher, vrfSigner, vrfVerifier, pd.nonceFetcher, pd,
			pd.msgTimes,
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

	config       Config
	nodeID       types.NodeID
	sync         system.SyncStateProvider
	publisher    pubsub.Publisher
	edSigner     *signing.EdSigner
	edVerifier   *signing.EdVerifier
	vrfSigner    vrfSigner
	vrfVerifier  vrfVerifier
	nonceFetcher nonceFetcher
	weakCoin     coin
	theta        *big.Float

	clock    layerClock
	msgTimes *messageTimes
	cdb      *datastore.CachedDB

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
	results        chan result.Beacon

	// metrics
	metricsCollector *metrics.BeaconMetricsCollector
	metricsRegistry  *prometheus.Registry
}

// SetSyncState updates sync state provider. Must be executed only once.
func (pd *ProtocolDriver) SetSyncState(sync system.SyncStateProvider) {
	if pd.sync != nil {
		pd.logger.Fatal("sync state provider can be updated only once")
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
			pd.logger.Fatal("update sync state provider can't be nil")
		}

		pd.metricsCollector.Start(nil)

		pd.setProposalTimeForNextEpoch()
		pd.eg.Go(func() error {
			pd.listenEpochs(ctx)
			return nil
		})
	})
}

func (pd *ProtocolDriver) UpdateBeacon(epoch types.EpochID, beacon types.Beacon) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if err := beacons.Set(pd.cdb, epoch, beacon); err != nil {
		return fmt.Errorf("persist fallback beacon epoch %v, beacon %v: %w", epoch, beacon, err)
	}
	pd.beacons[epoch] = beacon
	pd.logger.With().Info("using fallback beacon", epoch, beacon)
	pd.onResult(epoch, beacon)
	return nil
}

// Close closes ProtocolDriver.
func (pd *ProtocolDriver) Close() {
	pd.logger.Info("closing beacon protocol")
	pd.metricsCollector.Stop()
	pd.cancel()
	pd.logger.Info("waiting for beacon goroutines to finish")
	if err := pd.eg.Wait(); err != nil {
		pd.logger.With().Info("received error waiting for goroutines to finish", log.Err(err))
	}
	close(pd.results)
	pd.logger.Info("beacon goroutines finished")
}

func (pd *ProtocolDriver) onResult(epoch types.EpochID, beacon types.Beacon) {
	select {
	case pd.results <- result.Beacon{Epoch: epoch, Beacon: beacon}:
	default:
		pd.logger.With().Error("results queue is congested",
			log.Uint32("epoch_id", epoch.Uint32()),
			log.Stringer("beacon", beacon),
		)
	}
}

// Results notifies waiter that beacon for a target epoch has completed.
func (pd *ProtocolDriver) Results() <-chan result.Beacon {
	return pd.results
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

func (pd *ProtocolDriver) OnAtx(atx *types.ActivationTxHeader) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	s, ok := pd.states[atx.TargetEpoch()]
	if !ok {
		return
	}
	if id, ok := s.minerAtxs[atx.NodeID]; ok && id != atx.ID {
		pd.logger.With().Warning("ignoring malicious atx",
			log.Stringer("smesher", atx.NodeID),
			log.Stringer("previous_atx", id),
			log.Stringer("new_atx", atx.ID),
		)
		return
	}
	s.minerAtxs[atx.NodeID] = atx.ID
}

func (pd *ProtocolDriver) minerAtxHdr(epoch types.EpochID, nodeID types.NodeID) (*types.ActivationTxHeader, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	st, ok := pd.states[epoch]
	if !ok {
		return nil, errEpochNotActive
	}

	id, ok := st.minerAtxs[nodeID]
	if !ok {
		pd.logger.With().Debug("miner does not have atx in previous epoch",
			epoch-1,
			log.Stringer("smesher", nodeID),
		)
		return nil, errMinerNotActive
	}
	return pd.cdb.GetAtxHeader(id)
}

func (pd *ProtocolDriver) MinerAllowance(epoch types.EpochID, nodeID types.NodeID) uint32 {
	atx, err := pd.minerAtxHdr(epoch, nodeID)
	if err != nil {
		return 0
	}
	return atx.NumUnits
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
	numEligibility := len(ballot.EligibilityProofs)
	ballotWeight := weightPer.Mul(fixed.New(numEligibility))
	if _, ok := pd.ballotsBeacons[epochID]; !ok {
		pd.ballotsBeacons[epochID] = make(map[types.Beacon]*beaconWeight)
	}
	entry, ok := pd.ballotsBeacons[epochID][beacon]
	if !ok {
		pd.ballotsBeacons[epochID][beacon] = &beaconWeight{
			ballots:        map[types.BallotID]struct{}{ballot.ID(): {}},
			totalWeight:    ballotWeight,
			numEligibility: numEligibility,
		}
		pd.logger.With().Debug("added beacon from ballot",
			epochID,
			ballot.ID(),
			beacon,
			log.Stringer("weight_per", weightPer),
			log.Int("num_eligibility", numEligibility),
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
	entry.numEligibility += numEligibility
	pd.logger.With().Debug("added beacon from ballot",
		epochID,
		ballot.ID(),
		beacon,
		log.Stringer("weight_per", weightPer),
		log.Int("num_eligibility", numEligibility),
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
	totalEligibility := 0
	for _, bw := range epochBeacons {
		totalEligibility += bw.numEligibility
		totalWeight = totalWeight.Add(bw.totalWeight)
	}

	logger := pd.logger.WithFields(epoch, log.Int("total_weight_units", totalEligibility))
	if totalEligibility < pd.config.BeaconSyncWeightUnits {
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
				log.Int("total_weight_unit", totalEligibility),
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
		log.Int("total_weight_unit", totalEligibility),
		log.Stringer("total_weight", totalWeight),
		log.Stringer("beacon_weight", maxWeight))
	return bPlurality
}

// GetBeacon returns the beacon for the specified epoch or an error if it doesn't exist.
func (pd *ProtocolDriver) GetBeacon(targetEpoch types.EpochID) (types.Beacon, error) {
	beacon := pd.getBeacon(targetEpoch)
	if beacon != types.EmptyBeacon {
		return beacon, nil
	}

	beacon, err := pd.getPersistedBeacon(targetEpoch)
	if err == nil {
		return beacon, nil
	}

	if errors.Is(err, sql.ErrNotFound) {
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
	if beacon == types.EmptyBeacon {
		pd.logger.Fatal("invalid beacon")
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	if savedBeacon, ok := pd.beacons[targetEpoch]; ok {
		if beacon != savedBeacon {
			return errDifferentBeacon
		}
		return nil
	}

	if err := beacons.Add(pd.cdb, targetEpoch, beacon); err != nil && !errors.Is(err, sql.ErrObjectExists) {
		pd.logger.With().Error("failed to persist beacon", targetEpoch, beacon, log.Err(err))
		return fmt.Errorf("persist beacon: %w", err)
	}
	pd.beacons[targetEpoch] = beacon
	pd.onResult(targetEpoch, beacon)
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

	var (
		epochWeight uint64
		miners      = make(map[types.NodeID]types.ATXID)
		active      bool
		nonce       *types.VRFPostIndex
		// w1 is the weight units at δ before the end of the previous epoch, used to calculate `thresholdStrict`
		// w2 is the weight units at the end of the previous epoch, used to calculate `threshold`
		w1, w2 int
		ontime = pd.clock.LayerToTime(epoch.FirstLayer())
		early  = ontime.Add(-1 * pd.config.GracePeriodDuration)
	)
	if err := pd.cdb.IterateEpochATXHeaders(epoch, func(header *types.ActivationTxHeader) bool {
		epochWeight += header.GetWeight()
		if _, ok := miners[header.NodeID]; !ok {
			miners[header.NodeID] = header.ID
			if header.Received.Before(early) {
				w1++
			} else if header.Received.Before(ontime) {
				w2++
			}
		} else {
			pd.logger.With().Warning("ignoring malicious atx from miner",
				header.ID,
				log.Stringer("smesher", header.NodeID))
		}
		if header.NodeID == pd.nodeID {
			active = true
		}
		return true
	}); err != nil {
		return nil, err
	}

	if epochWeight == 0 {
		logger.With().Error("zero weight targeting epoch", log.Err(errZeroEpochWeight))
		return nil, errZeroEpochWeight
	}

	if active {
		nnc, err := pd.nonceFetcher.VRFNonce(pd.nodeID, epoch)
		if err != nil {
			logger.With().Error("failed to get own VRF nonce", log.Err(err))
			return nil, fmt.Errorf("get own VRF nonce: %w", err)
		}
		nonce = &nnc
	}
	checker := createProposalChecker(logger, pd.config, w1, w1+w2)
	pd.states[epoch] = newState(logger, pd.config, nonce, epochWeight, miners, checker)
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

func (pd *ProtocolDriver) setupEpoch(logger log.Log, epoch types.EpochID) (*state, error) {
	s, err := pd.initEpochStateIfNotPresent(logger, epoch)
	if err != nil {
		return nil, err
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.roundInProgress = types.FirstRound
	pd.earliestVoteTime = time.Time{}

	return s, nil
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
func (pd *ProtocolDriver) listenEpochs(ctx context.Context) {
	pd.logger.With().Info("starting listening layers")

	currentEpoch := pd.clock.CurrentLayer().GetEpoch()
	layer := currentEpoch.Add(1).FirstLayer()
	for {
		select {
		case <-pd.ctx.Done():
			return
		case <-pd.clock.AwaitLayer(layer):
			current := pd.clock.CurrentLayer()
			if current.Before(layer) {
				pd.logger.With().Info("time sync detected, realigning Beacon")
				continue
			}
			if !current.FirstInEpoch() {
				continue
			}
			epoch := current.GetEpoch()
			layer = epoch.Add(1).FirstLayer()

			pd.setProposalTimeForNextEpoch()
			pd.logger.With().Info("processing epoch", current, epoch)
			pd.eg.Go(func() error {
				_ = pd.onNewEpoch(ctx, epoch)
				return nil
			})
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

	if epoch.FirstLayer() <= types.GetEffectiveGenesis() {
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

	s, err := pd.setupEpoch(logger, epoch)
	if err != nil {
		logger.With().Error("failed to set up epoch", log.Err(err))
		return err
	}
	logger.With().Info("participating in beacon protocol")
	pd.runProtocol(ctx, epoch, s)
	return nil
}

func (pd *ProtocolDriver) runProtocol(ctx context.Context, epoch types.EpochID, st *state) {
	ctx = log.WithNewSessionID(ctx)
	targetEpoch := epoch + 1
	logger := pd.logger.WithContext(ctx).WithFields(epoch, log.Uint32("target_epoch", uint32(targetEpoch)))

	pd.setBeginProtocol(ctx)
	defer pd.setEndProtocol(ctx)

	pd.weakCoin.StartEpoch(ctx, epoch)
	defer pd.weakCoin.FinishEpoch(ctx, epoch)

	if err := pd.runProposalPhase(ctx, epoch, st); err != nil {
		logger.With().Warning("proposal phase failed", log.Err(err))
		return
	}
	lastRoundOwnVotes, err := pd.runConsensusPhase(ctx, epoch, st.nonce)
	if err != nil {
		logger.With().Warning("consensus phase failed", log.Err(err))
		return
	}
	if len(lastRoundOwnVotes.support) == 0 {
		logger.With().Warning("consensus phase failed", log.Err(errNoProposals))
		return
	}

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	beacon := calcBeacon(logger, lastRoundOwnVotes.support)

	if err = pd.setBeacon(targetEpoch, beacon); err != nil {
		logger.With().Error("failed to set beacon", log.Err(err))
		return
	}

	logger.With().Info("beacon set for epoch", beacon)
}

func calcBeacon(logger log.Log, set proposalSet) types.Beacon {
	allProposals := set.sort()
	allHexes := make([]string, len(allProposals))
	for i, h := range allProposals {
		allHexes[i] = hex.EncodeToString(h[:])
	}
	// Beacon should appear to have the same entropy as the initial proposals, hence cropping it
	// to the same size as the proposal
	beacon := types.BytesToBeacon(allProposals.hash().Bytes())
	logger.With().Info("calculated beacon",
		beacon,
		log.Int("num_hashes", len(allProposals)),
		log.String("proposals", strings.Join(allHexes, ", ")),
	)
	return beacon
}

func (pd *ProtocolDriver) runProposalPhase(ctx context.Context, epoch types.EpochID, st *state) error {
	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	logger.Info("starting beacon proposal phase")

	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, pd.config.ProposalDuration)
	defer cancel()

	if st.nonce != nil {
		pd.eg.Go(func() error {
			pd.sendProposal(ctx, epoch, *st.nonce, st.proposalChecker)
			return nil
		})
	}

	select {
	case <-ctx.Done():
	case <-pd.ctx.Done():
		return pd.ctx.Err()
	}

	if err := pd.markProposalPhaseFinished(epoch, time.Now()); err != nil {
		return err
	}

	logger.Info("beacon proposal phase finished")
	return nil
}

func (pd *ProtocolDriver) sendProposal(ctx context.Context, epoch types.EpochID, nonce types.VRFPostIndex, checker eligibilityChecker) {
	if pd.isClosed() {
		return
	}

	atx, err := pd.minerAtxHdr(epoch, pd.edSigner.NodeID())
	if err != nil {
		return
	}

	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	vrfSig := buildSignedProposal(ctx, pd.logger, pd.vrfSigner, epoch, nonce)
	proposal := ProposalFromVrf(vrfSig)
	m := ProposalMessage{
		EpochID:      epoch,
		NodeID:       pd.nodeID,
		VRFSignature: vrfSig,
	}

	if invalid == pd.classifyProposal(logger, m, atx.Received, time.Now(), checker) {
		logger.With().Debug("own proposal doesn't pass threshold",
			log.String("proposal", hex.EncodeToString(proposal[:])),
		)
		return
	}

	logger.With().Debug("own proposal passes threshold",
		log.String("proposal", hex.EncodeToString(proposal[:])),
	)

	serialized, err := codec.Encode(&m)
	if err != nil {
		logger.With().Fatal("failed to encode beacon proposal", log.Err(err))
	}

	pd.sendToGossip(ctx, pubsub.BeaconProposalProtocol, serialized)
	logger.With().Info("beacon proposal sent", log.String("proposal", hex.EncodeToString(proposal[:])))
}

// runConsensusPhase runs K voting rounds and returns result from last weak coin round.
func (pd *ProtocolDriver) runConsensusPhase(ctx context.Context, epoch types.EpochID, nonce *types.VRFPostIndex) (allVotes, error) {
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
		undecided proposalList
		err       error
	)
	for round := types.FirstRound; round < pd.config.RoundsNumber; round++ {
		round := round
		pd.setRoundInProgress(round)
		rLogger := logger.WithFields(round)
		votes := ownVotes
		if nonce != nil {
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
		}

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
				pd.weakCoin.StartRound(ctx, round, nonce)
				return nil
			})
			select {
			case <-timer.C:
			case <-ctx.Done():
				return allVotes{}, fmt.Errorf("context done: %w", ctx.Err())
			}
			pd.weakCoin.FinishRound(ctx)
			flip, err := pd.weakCoin.Get(ctx, epoch, round)
			if err != nil {
				rLogger.With().Error("failed to generate weak coin", log.Err(err))
				return allVotes{}, err
			}
			tallyUndecided(&ownVotes, undecided, flip)
		}
		timer.Reset(pd.config.VotingRoundDuration)
	}

	logger.Info("consensus phase finished")
	return ownVotes, nil
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

func (pd *ProtocolDriver) calcVotesBeforeWeakCoin(logger log.Log, epoch types.EpochID) (allVotes, proposalList, error) {
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
		pd.logger.With().Fatal("failed to serialize message for signing", log.Err(err))
	}
	sig := pd.edSigner.Sign(signing.BEACON_FIRST_MSG, encoded)

	m := FirstVotingMessage{
		FirstVotingMessageBody: mb,
		SmesherID:              pd.edSigner.NodeID(),
		Signature:              sig,
	}

	pd.logger.WithContext(ctx).With().Debug("sending first round vote", epoch, types.FirstRound)
	serialized, err := codec.Encode(&m)
	if err != nil {
		pd.logger.With().Fatal("failed to serialize message for gossip", log.Err(err))
	}

	pd.sendToGossip(ctx, pubsub.BeaconFirstVotesProtocol, serialized)
	return nil
}

func (pd *ProtocolDriver) getFirstRoundVote(epoch types.EpochID, nodeID types.NodeID) (proposalList, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	st, ok := pd.states[epoch]
	if !ok {
		return nil, errEpochNotActive
	}

	return st.getMinerFirstRoundVote(nodeID)
}

func (pd *ProtocolDriver) sendFollowingVote(ctx context.Context, epoch types.EpochID, round types.RoundID, ownCurrentRoundVotes allVotes) error {
	firstRoundVotes, err := pd.getFirstRoundVote(epoch, pd.edSigner.NodeID())
	if err != nil {
		return fmt.Errorf("get own first round votes %v: %w", pd.edSigner.NodeID().String(), err)
	}

	bitVector := encodeVotes(ownCurrentRoundVotes, firstRoundVotes)
	mb := FollowingVotingMessageBody{
		EpochID:        epoch,
		RoundID:        round,
		VotesBitVector: bitVector,
	}

	encoded, err := codec.Encode(&mb)
	if err != nil {
		pd.logger.With().Fatal("failed to serialize message for signing", log.Err(err))
	}
	sig := pd.edSigner.Sign(signing.BEACON_FOLLOWUP_MSG, encoded)

	m := FollowingVotingMessage{
		FollowingVotingMessageBody: mb,
		SmesherID:                  pd.edSigner.NodeID(),
		Signature:                  sig,
	}

	pd.logger.WithContext(ctx).With().Debug("sending following round vote", epoch, round)

	serialized, err := codec.Encode(&m)
	if err != nil {
		pd.logger.With().Fatal("failed to serialize message for gossip", log.Err(err))
	}

	pd.sendToGossip(ctx, pubsub.BeaconFollowingVotesProtocol, serialized)
	return nil
}

type proposalChecker struct {
	threshold       *big.Int
	thresholdStrict *big.Int
}

func createProposalChecker(logger log.Log, conf Config, numEarlyATXs, numATXs int) eligibilityChecker {
	if numATXs == 0 {
		logger.Error("creating proposal checker with zero atxs")
		return &proposalChecker{threshold: big.NewInt(0), thresholdStrict: big.NewInt(0)}
	}

	high := atxThreshold(conf.Kappa, conf.Q, numEarlyATXs)
	low := atxThreshold(conf.Kappa, conf.Q, numATXs)
	logger.With().Info("created proposal checker with ATX threshold",
		log.Int("num_early_atxs", numEarlyATXs),
		log.Int("num_atxs", numATXs),
		log.String("threshold", high.String()),
		log.String("threshold_strict", low.String()))
	return &proposalChecker{threshold: high, thresholdStrict: low}
}

func (pc *proposalChecker) PassStrictThreshold(proposal types.VrfSignature) bool {
	proposalInt := new(big.Int).SetBytes(proposal[:])
	return proposalInt.Cmp(pc.thresholdStrict) == -1
}

func (pc *proposalChecker) PassThreshold(proposal types.VrfSignature) bool {
	proposalInt := new(big.Int).SetBytes(proposal[:])
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

func buildSignedProposal(ctx context.Context, logger log.Log, signer vrfSigner, epoch types.EpochID, nonce types.VRFPostIndex) types.VrfSignature {
	p := buildProposal(logger, epoch, nonce)
	vrfSig := signer.Sign(p)
	proposal := ProposalFromVrf(vrfSig)
	logger.WithContext(ctx).With().Debug("calculated beacon proposal",
		epoch,
		nonce,
		log.String("proposal", hex.EncodeToString(proposal[:])),
	)
	return vrfSig
}

func buildProposal(logger log.Log, epoch types.EpochID, nonce types.VRFPostIndex) []byte {
	message := &ProposalVrfMessage{
		Type:  types.EligibilityBeacon,
		Nonce: nonce,
		Epoch: epoch,
	}

	b, err := codec.Encode(message)
	if err != nil {
		logger.With().Fatal("failed to serialize proposal", log.Err(err))
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

	epoch := pd.clock.CurrentLayer().GetEpoch()
	var observed []*metrics.BeaconStats
	if epochBeacons, ok := pd.ballotsBeacons[epoch]; ok {
		for beacon, stats := range epochBeacons {
			stat := &metrics.BeaconStats{
				Epoch:      epoch,
				Beacon:     beacon.ShortString(),
				Weight:     stats.totalWeight,
				WeightUnit: stats.numEligibility,
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

// messageTimes provides methods to determine the intended send time of
// messages spceific to the beacon protocol.
type messageTimes struct {
	clock layerClock
	conf  Config
}

// proposalSendTime returns the time at which a proposal is sent for an epoch.
func (mt *messageTimes) proposalSendTime(epoch types.EpochID) time.Time {
	return mt.clock.LayerToTime(epoch.FirstLayer())
}

// firstVoteSendTime returns the time at which the first vote is sent for an epoch.
func (mt *messageTimes) firstVoteSendTime(epoch types.EpochID) time.Time {
	return mt.proposalSendTime(epoch).Add(mt.conf.ProposalDuration)
}

// followupVoteSendTime returns the time at which the followup votes are sent for an epoch and round.
func (mt *messageTimes) followupVoteSendTime(epoch types.EpochID, round types.RoundID) time.Time {
	subsequentRoundDuration := mt.conf.VotingRoundDuration + mt.conf.WeakCoinRoundDuration
	return mt.firstVoteSendTime(epoch).Add(mt.conf.FirstVotingRoundDuration).Add(subsequentRoundDuration * time.Duration(round-1))
}

// WeakCoinProposalSendTime returns the time at which the weak coin proposals are sent for an epoch and round.
func (mt *messageTimes) WeakCoinProposalSendTime(epoch types.EpochID, round types.RoundID) time.Time {
	subsequentRoundDuration := mt.conf.VotingRoundDuration + mt.conf.WeakCoinRoundDuration
	return mt.firstVoteSendTime(epoch).Add(mt.conf.FirstVotingRoundDuration + mt.conf.VotingRoundDuration).Add(subsequentRoundDuration * time.Duration(round-1))
}
