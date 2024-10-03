package beacon

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ALTree/bigfloat"
	"github.com/spacemeshos/fixed"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/beacon/metrics"
	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
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

// Opt for configuring beacon protocol.
type Opt func(*ProtocolDriver)

// WithLogger defines logger for beacon.
func WithLogger(logger *zap.Logger) Opt {
	return func(pd *ProtocolDriver) {
		pd.logger = logger
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
	publisher pubsub.Publisher,
	edVerifier *signing.EdVerifier,
	vrfVerifier vrfVerifier,
	cdb *datastore.CachedDB,
	clock layerClock,
	opts ...Opt,
) *ProtocolDriver {
	pd := &ProtocolDriver{
		logger:         zap.NewNop(),
		config:         DefaultConfig(),
		publisher:      publisher,
		edVerifier:     edVerifier,
		vrfVerifier:    vrfVerifier,
		nonceFetcher:   cdb,
		cdb:            cdb,
		clock:          clock,
		signers:        make(map[types.NodeID]*signing.EdSigner),
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: make(map[types.EpochID]map[types.Beacon]*beaconWeight),
		states:         make(map[types.EpochID]*state),
		results:        make(chan result.Beacon, 100),
		closed:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(pd)
	}
	pd.msgTimes = &messageTimes{
		clock: clock,
		conf:  pd.config,
	}

	pd.theta = new(big.Float).SetRat(&pd.config.Theta)

	if pd.weakCoin == nil {
		pd.weakCoin = weakcoin.New(
			pd.publisher,
			pd.vrfVerifier,
			pd.nonceFetcher,
			pd,
			pd.msgTimes,
			weakcoin.WithLog(pd.logger.Named("weakCoin")),
			weakcoin.WithMaxRound(pd.config.RoundsNumber),
		)
	}

	pd.metricsCollector = metrics.NewBeaconMetricsCollector(pd.gatherMetricsData, pd.logger.Named("metrics"))
	return pd
}

func (pd *ProtocolDriver) Register(sig *signing.EdSigner) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	if _, exists := pd.signers[sig.NodeID()]; exists {
		pd.logger.Error("signing key already registered", log.ZShortStringer("node_id", sig.NodeID()))
		return
	}

	pd.logger.Info("registered signing key", log.ZShortStringer("node_id", sig.NodeID()))
	pd.signers[sig.NodeID()] = sig
}

type participant struct {
	signer *signing.EdSigner
	nonce  types.VRFPostIndex
}

func (s participant) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("node_id", s.signer.NodeID().ShortString())
	enc.AddUint64("nonce", uint64(s.nonce))
	return nil
}

// ProtocolDriver is the driver for the beacon protocol.
type ProtocolDriver struct {
	inProtocol uint64
	logger     *zap.Logger
	eg         errgroup.Group
	startOnce  sync.Once
	closed     chan struct{}

	config    Config
	sync      system.SyncStateProvider
	publisher pubsub.Publisher

	signers  map[types.NodeID]*signing.EdSigner
	weakCoin coin

	edVerifier   *signing.EdVerifier
	vrfVerifier  vrfVerifier
	nonceFetcher nonceFetcher
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

	resultsMtx sync.Mutex
	results    chan result.Beacon

	// metrics
	metricsCollector *metrics.BeaconMetricsCollector
}

// SetSyncState updates sync state provider. Must be executed only once.
func (pd *ProtocolDriver) SetSyncState(sync system.SyncStateProvider) {
	if pd.sync != nil {
		pd.logger.Fatal("sync state provider can be updated only once")
	}
	pd.sync = sync
}

// Start starts listening for layers and outputs.
func (pd *ProtocolDriver) Start(ctx context.Context) {
	pd.startOnce.Do(func() {
		if pd.sync == nil {
			pd.logger.Fatal("update sync state provider can't be nil")
		}
		pd.metricsCollector.Start(nil)

		if pd.config.RoundsNumber == 0 {
			pd.logger.Info("beacon protocol disabled")
			return
		}
		pd.logger.Info("starting beacon protocol", zap.Any("config", pd.config))
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
	pd.logger.Info("using fallback beacon",
		zap.Uint32("epoch", epoch.Uint32()),
		zap.Stringer("beacon", beacon),
	)
	pd.onResult(epoch, beacon)
	return nil
}

// Close closes ProtocolDriver.
func (pd *ProtocolDriver) Close() {
	pd.resultsMtx.Lock()
	defer pd.resultsMtx.Unlock()

	if pd.isClosed() {
		return
	}

	pd.logger.Info("closing beacon protocol")
	pd.metricsCollector.Stop()
	close(pd.closed)
	pd.logger.Debug("waiting for beacon goroutines to finish")
	if err := pd.eg.Wait(); err != nil {
		pd.logger.Warn("received error waiting for goroutines to finish", zap.Error(err))
	}
	close(pd.results)
	pd.results = nil
	pd.logger.Debug("beacon goroutines finished")
}

func (pd *ProtocolDriver) onResult(epoch types.EpochID, beacon types.Beacon) {
	pd.resultsMtx.Lock()
	defer pd.resultsMtx.Unlock()
	if pd.results == nil {
		return
	}

	select {
	case pd.results <- result.Beacon{Epoch: epoch, Beacon: beacon}:
	default:
		pd.logger.Error("results queue is congested",
			zap.Uint32("epoch", epoch.Uint32()),
			zap.Stringer("beacon", beacon),
		)
	}
}

// Results notifies waiter that beacon for a target epoch has completed.
func (pd *ProtocolDriver) Results() <-chan result.Beacon {
	pd.resultsMtx.Lock()
	defer pd.resultsMtx.Unlock()
	return pd.results
}

// isClosed returns true if the beacon protocol is shutting down.
func (pd *ProtocolDriver) isClosed() bool {
	select {
	case <-pd.closed:
		return true
	default:
		return false
	}
}

func (pd *ProtocolDriver) OnAtx(atx *types.ActivationTx) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	s, ok := pd.states[atx.TargetEpoch()]
	if !ok {
		return
	}
	if mi, ok := s.minerAtxs[atx.SmesherID]; ok && mi.atxid != atx.ID() {
		pd.logger.Warn("ignoring malicious atx",
			zap.Stringer("smesher", atx.SmesherID),
			zap.Stringer("previous_atx", mi.atxid),
			zap.Stringer("new_atx", atx.ID()),
		)
		return
	}
	s.minerAtxs[atx.SmesherID] = &minerInfo{
		atxid: atx.ID(),
	}
}

func (pd *ProtocolDriver) minerAtxHdr(
	epoch types.EpochID,
	nodeID types.NodeID,
) (*types.ActivationTx, bool, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	st, ok := pd.states[epoch]
	if !ok {
		return nil, false, errEpochNotActive
	}

	mi, ok := st.minerAtxs[nodeID]
	if !ok {
		pd.logger.Debug("miner does not have atx in previous epoch",
			zap.Uint32("previous_epoch", uint32(epoch-1)),
			zap.Stringer("smesher", nodeID),
		)
		return nil, false, errMinerNotActive
	}
	hdr, err := pd.cdb.GetAtx(mi.atxid)
	if err != nil {
		return nil, false, fmt.Errorf("get miner atx hdr %v: %w", mi.atxid.ShortString(), err)
	}
	return hdr, mi.malicious, nil
}

func (pd *ProtocolDriver) MinerAllowance(epoch types.EpochID, nodeID types.NodeID) uint32 {
	atx, malicious, err := pd.minerAtxHdr(epoch, nodeID)
	if err != nil || malicious {
		return 0
	}
	return atx.NumUnits
}

// ReportBeaconFromBallot reports the beacon value in a ballot along with the smesher's weight unit.
func (pd *ProtocolDriver) ReportBeaconFromBallot(
	epoch types.EpochID,
	ballot *types.Ballot,
	beacon types.Beacon,
	weightPer fixed.Fixed,
) {
	pd.recordBeacon(epoch, ballot, beacon, weightPer)

	if _, err := pd.GetBeacon(epoch); err == nil {
		// already has beacon. i.e. we had participated in the beacon protocol during the last epoch
		return
	}

	if eBeacon := pd.findMajorityBeacon(epoch); eBeacon != types.EmptyBeacon {
		if err := pd.setBeacon(epoch, eBeacon); err != nil {
			pd.logger.Error("beacon sync: failed to set beacon", zap.Error(err))
		}
	}
}

func (pd *ProtocolDriver) recordBeacon(
	epochID types.EpochID,
	ballot *types.Ballot,
	beacon types.Beacon,
	weightPer fixed.Fixed,
) {
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
		pd.logger.Debug("added beacon from ballot",
			zap.Uint32("epoch", epochID.Uint32()),
			zap.Stringer("ballotID", ballot.ID()),
			zap.Stringer("beacon", beacon),
			zap.Stringer("weight_per", weightPer),
			zap.Int("num_eligibility", numEligibility),
			zap.Stringer("weight", ballotWeight),
		)
		return
	}

	// checks if we have recorded this ballot before
	if _, ok = entry.ballots[ballot.ID()]; ok {
		pd.logger.Warn("ballot already reported beacon",
			zap.Uint32("epoch", epochID.Uint32()),
			zap.Stringer("ballotID", ballot.ID()),
		)
		return
	}

	entry.ballots[ballot.ID()] = struct{}{}
	entry.totalWeight = entry.totalWeight.Add(ballotWeight)
	entry.numEligibility += numEligibility
	pd.logger.Debug("added beacon from ballot",
		zap.Uint32("epoch", epochID.Uint32()),
		zap.Stringer("ballotID", ballot.ID()),
		zap.Stringer("beacon", beacon),
		zap.Stringer("weight_per", weightPer),
		zap.Int("num_eligibility", numEligibility),
		zap.Stringer("weight", ballotWeight),
	)
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

	logger := pd.logger.With(
		zap.Uint32("epoch", epoch.Uint32()),
		zap.Int("total_weight_units", totalEligibility),
	)
	if totalEligibility < pd.config.BeaconSyncWeightUnits {
		logger.Debug("not enough weight units to determine beacon")
		return types.EmptyBeacon
	}

	majorityWeight := totalWeight.Div(fixed.New(2))
	maxWeight := fixed.New64(0)
	var bPlurality types.Beacon
	for beacon, bw := range epochBeacons {
		if bw.totalWeight.GreaterThan(majorityWeight) {
			logger.Info("beacon determined for epoch",
				zap.Stringer("beacon", beacon),
				zap.Int("total_weight_unit", totalEligibility),
				zap.Stringer("total_weight", totalWeight),
				zap.Stringer("beacon_weight", bw.totalWeight),
			)
			return beacon
		}
		if bw.totalWeight.GreaterThan(maxWeight) {
			bPlurality = beacon
			maxWeight = bw.totalWeight
		}
	}
	logger.Warn("beacon determined for epoch by plurality, not majority",
		zap.Stringer("beacon", bPlurality),
		zap.Int("total_weight_unit", totalEligibility),
		zap.Stringer("total_weight", totalWeight),
		zap.Stringer("beacon_weight", maxWeight),
	)
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
	pd.logger.Error("failed to get beacon from db",
		zap.Uint32("target_epoch", targetEpoch.Uint32()),
		zap.Error(err),
	)
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
		pd.logger.Error("failed to persist beacon",
			zap.Uint32("target_epoch", targetEpoch.Uint32()),
			zap.Stringer("beacon", beacon),
			zap.Error(err),
		)
		return fmt.Errorf("persist beacon: %w", err)
	}
	pd.beacons[targetEpoch] = beacon
	pd.onResult(targetEpoch, beacon)
	curr := pd.clock.CurrentLayer().GetEpoch()
	switch targetEpoch {
	case curr:
		public.CurrentBeacon.WithLabelValues(beacon.String())
	case curr + 1:
		public.NextBeacon.WithLabelValues(beacon.String())
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
		pd.logger.Error("attempt to begin the beacon protocol more than once",
			log.ZContext(ctx),
		)
	}
}

func (pd *ProtocolDriver) setEndProtocol(ctx context.Context) {
	if !atomic.CompareAndSwapUint64(&pd.inProtocol, 1, 0) {
		pd.logger.Error("attempt to end the beacon protocol more than once",
			log.ZContext(ctx),
		)
	}
}

func (pd *ProtocolDriver) isInProtocol() bool {
	return atomic.LoadUint64(&pd.inProtocol) == 1
}

func (pd *ProtocolDriver) initEpochStateIfNotPresent(logger *zap.Logger, target types.EpochID) (*state, error) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if s, ok := pd.states[target]; ok {
		return s, nil
	}

	var (
		epochWeight       uint64
		miners            = make(map[types.NodeID]*minerInfo)
		potentiallyActive = make(map[types.NodeID]*signing.EdSigner)
		// w1 is the weight units at δ before the end of the previous epoch, used to calculate `thresholdStrict`
		// w2 is the weight units at the end of the previous epoch, used to calculate `threshold`
		w1, w2 int
		ontime = pd.clock.LayerToTime(target.FirstLayer())
		early  = ontime.Add(-1 * pd.config.GracePeriodDuration)
	)
	err := atxs.IterateAtxsWithMalfeasance(pd.cdb, target-1, func(atx *types.ActivationTx, malicious bool) bool {
		if !malicious {
			epochWeight += atx.Weight
		} else {
			logger.Debug("malicious miner get 0 weight", zap.Stringer("smesher", atx.SmesherID))
		}
		if _, ok := miners[atx.SmesherID]; !ok {
			miners[atx.SmesherID] = &minerInfo{
				atxid:     atx.ID(),
				malicious: malicious,
			}
			if atx.Received().Before(early) {
				w1++
			} else if atx.Received().Before(ontime) {
				w2++
			}
		} else {
			logger.Warn("ignoring malicious atx from miner",
				zap.Stringer("atx_id", atx.ID()),
				zap.Bool("malicious", malicious),
				zap.Stringer("smesher", atx.SmesherID),
			)
		}

		if s, ok := pd.signers[atx.SmesherID]; ok {
			potentiallyActive[atx.SmesherID] = s
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	if epochWeight == 0 {
		return nil, errZeroEpochWeight
	}

	active := map[types.NodeID]participant{}
	for id, signer := range potentiallyActive {
		if nonce, err := pd.nonceFetcher.VRFNonce(id, target); err != nil {
			logger.Error("getting own VRF nonce", zap.Stringer("node_id", id), zap.Error(err))
		} else {
			active[id] = participant{
				signer: signer,
				nonce:  nonce,
			}
		}
	}

	logger.Debug("selected active signers",
		zap.Int("count", len(active)),
		zap.Array("signers", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
			for _, p := range active {
				enc.AppendObject(p)
			}
			return nil
		})),
	)

	checker := createProposalChecker(logger, pd.config, w1, w1+w2)
	pd.states[target] = newState(logger, active, epochWeight, miners, checker)
	return pd.states[target], nil
}

func (pd *ProtocolDriver) setProposalTimeForNextEpoch() {
	epoch := pd.currentEpoch()
	nextEpochStart := pd.clock.LayerToTime((epoch + 1).FirstLayer())
	t := nextEpochStart.Add(-pd.config.GracePeriodDuration)
	pd.logger.Debug("earliest proposal time for epoch",
		zap.Uint32("next_epoch", uint32(epoch+1)),
		zap.Time("earliest_time", t),
	)

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.earliestProposalTime = t
}

func (pd *ProtocolDriver) setupEpoch(logger *zap.Logger, epoch types.EpochID) (*state, error) {
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
	pd.logger.Info("starting listening layers")

	currentEpoch := pd.clock.CurrentLayer().GetEpoch()
	layer := currentEpoch.Add(1).FirstLayer()
	for {
		select {
		case <-pd.closed:
			return
		case <-pd.clock.AwaitLayer(layer):
			current := pd.clock.CurrentLayer()
			if current.Before(layer) {
				pd.logger.Debug("time sync detected, realigning Beacon")
				continue
			}
			if !current.FirstInEpoch() {
				continue
			}
			epoch := current.GetEpoch()
			layer = epoch.Add(1).FirstLayer()

			pd.setProposalTimeForNextEpoch()
			pd.logger.Info("processing epoch", zap.Uint32("epoch", epoch.Uint32()))
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
	pd.logger.Debug("earliest vote time for next round",
		zap.Uint32("round", uint32(round)),
		zap.Uint32("next_round", uint32(round+1)),
		zap.Time("earliest_time", earliestVoteTime),
	)

	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.roundInProgress = round
	pd.earliestVoteTime = earliestVoteTime
}

func (pd *ProtocolDriver) onNewEpoch(ctx context.Context, epoch types.EpochID) error {
	logger := pd.logger.With(
		log.ZContext(ctx),
		zap.Uint32("epoch", epoch.Uint32()),
	)
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
		logger.Error("failed to set up epoch", zap.Error(err))
		return err
	}
	logger.Debug("participating in beacon protocol")
	pd.runProtocol(ctx, epoch, s)
	return nil
}

func (pd *ProtocolDriver) runProtocol(ctx context.Context, epoch types.EpochID, st *state) {
	ctx = log.WithNewSessionID(ctx)
	targetEpoch := epoch + 1
	logger := pd.logger.With(
		log.ZContext(ctx),
		zap.Uint32("epoch", epoch.Uint32()),
		zap.Uint32("target_epoch", targetEpoch.Uint32()),
	)

	pd.setBeginProtocol(ctx)
	defer pd.setEndProtocol(ctx)

	pd.weakCoin.StartEpoch(ctx, epoch)
	defer pd.weakCoin.FinishEpoch(ctx, epoch)

	if err := pd.runProposalPhase(ctx, epoch, st); err != nil {
		logger.Warn("proposal phase failed", zap.Error(err))
		return
	}
	lastRoundOwnVotes, err := pd.runConsensusPhase(ctx, epoch, st)
	if err != nil {
		logger.Warn("consensus phase failed", zap.Error(err))
		return
	}
	if len(lastRoundOwnVotes.support) == 0 {
		logger.Warn("consensus phase failed", zap.Error(errNoProposals))
		return
	}

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	beacon := calcBeacon(logger, lastRoundOwnVotes.support)

	if err = pd.setBeacon(targetEpoch, beacon); err != nil {
		logger.Error("failed to set beacon", zap.Error(err))
		return
	}

	logger.Info("beacon set for epoch", zap.Stringer("beacon", beacon))
}

func calcBeacon(logger *zap.Logger, set proposalSet) types.Beacon {
	allProposals := set.sorted()

	// Beacon should appear to have the same entropy as the initial proposals, hence cropping it
	// to the same size as the proposal
	beacon := types.BytesToBeacon(allProposals.hash().Bytes())
	logger.Info("calculated beacon",
		zap.Stringer("beacon", beacon),
		zap.Int("num_hashes", len(allProposals)),
		zap.Array("proposals", allProposals),
	)
	return beacon
}

func (pd *ProtocolDriver) runProposalPhase(ctx context.Context, epoch types.EpochID, st *state) error {
	logger := pd.logger.With(
		log.ZContext(ctx),
		zap.Uint32("epoch", epoch.Uint32()),
	)
	logger.Debug("starting beacon proposal phase")

	ctx, cancel := context.WithTimeout(ctx, pd.config.ProposalDuration)
	defer cancel()

	for _, session := range st.active {
		pd.eg.Go(func() error {
			pd.sendProposal(ctx, epoch, session, st.proposalChecker)
			return nil
		})
	}

	select {
	case <-ctx.Done():
	case <-pd.closed:
		return errors.New("protocol closed")
	}

	finished := time.Now()
	pd.markProposalPhaseFinished(st, finished)
	logger.Debug("proposal phase finished", zap.Time("finished_at", finished))
	return nil
}

func (pd *ProtocolDriver) sendProposal(
	ctx context.Context,
	epoch types.EpochID,
	s participant,
	checker eligibilityChecker,
) {
	if pd.isClosed() {
		return
	}

	atx, malicious, err := pd.minerAtxHdr(epoch, s.signer.NodeID())
	if err != nil || malicious {
		return
	}

	logger := pd.logger.With(log.ZContext(ctx), zap.Uint32("epoch", epoch.Uint32()))
	vrfSig := buildSignedProposal(ctx, pd.logger, s.signer.VRFSigner(), epoch, s.nonce)
	proposal := ProposalFromVrf(vrfSig)
	m := ProposalMessage{
		EpochID:      epoch,
		NodeID:       s.signer.NodeID(),
		VRFSignature: vrfSig,
	}

	if invalid == pd.classifyProposal(logger, m, atx.Received(), time.Now(), checker) {
		logger.Debug("own proposal doesn't pass threshold",
			zap.Inline(proposal),
			log.ZShortStringer("node_id", s.signer.NodeID()),
		)
		return
	}

	logger.Debug("own proposal passes threshold",
		zap.Inline(proposal),
		log.ZShortStringer("node_id", s.signer.NodeID()),
	)
	if err := pd.sendToGossip(ctx, pubsub.BeaconProposalProtocol, codec.MustEncode(&m)); err != nil {
		logger.Error("failed to broadcast",
			zap.Error(err),
			zap.Inline(proposal),
			log.ZShortStringer("node_id", s.signer.NodeID()),
		)
	} else {
		logger.Debug("beacon proposal sent",
			zap.Inline(proposal),
			log.ZShortStringer("node_id", s.signer.NodeID()),
		)
	}
}

// runConsensusPhase runs K voting rounds and returns result from last weak coin round.
func (pd *ProtocolDriver) runConsensusPhase(ctx context.Context, epoch types.EpochID, st *state) (allVotes, error) {
	logger := pd.logger.With(log.ZContext(ctx), zap.Uint32("epoch", epoch.Uint32()))
	logger.Debug("starting consensus phase")

	// For K rounds: In each round that lasts δ, wait for votes to come in.
	// For next rounds,
	// wait for δ time, and construct a message that points to all messages from previous round received by δ.
	// rounds 1 to K
	timer := time.NewTimer(pd.config.FirstVotingRoundDuration)
	defer timer.Stop()

	var (
		ownVotes  allVotes
		undecided proposalList
	)

	// First round
	round := types.FirstRound
	pd.setRoundInProgress(round)
	pd.mu.RLock() // shared lock is fine as sorting doesn't modify the state
	msg := FirstVotingMessageBody{
		EpochID:                   epoch,
		ValidProposals:            st.incomingProposals.valid.sorted(),
		PotentiallyValidProposals: st.incomingProposals.potentiallyValid.sorted(),
	}
	pd.mu.RUnlock()
	for _, session := range st.active {
		pd.eg.Go(func() error {
			if err := pd.sendFirstRoundVote(ctx, msg, session.signer); err != nil {
				logger.Error("failed to send proposal vote",
					zap.Error(err),
					log.ZShortStringer("smesherID", session.signer.NodeID()),
					zap.Uint32("round", uint32(round)),
				)
			}
			return nil
		})
	}

	select {
	case <-timer.C:
	case <-ctx.Done():
		return allVotes{}, fmt.Errorf("context done: %w", ctx.Err())
	}
	ownVotes, _ = pd.calcVotesBeforeWeakCoin(logger, st)

	// Subsequent rounds
	for round := types.FirstRound + 1; round < pd.config.RoundsNumber; round++ {
		pd.setRoundInProgress(round)
		rLogger := logger.With(zap.Uint32("round", uint32(round)))
		timer.Reset(pd.config.VotingRoundDuration)

		votes := ownVotes
		for _, session := range st.active {
			pd.eg.Go(func() error {
				if err := pd.sendFollowingVote(ctx, epoch, round, votes, session.signer); err != nil {
					logger.Error("failed to send following vote",
						zap.Error(err),
						log.ZShortStringer("smesherID", session.signer.NodeID()),
					)
				}
				return nil
			})
		}

		select {
		case <-timer.C:
		case <-ctx.Done():
			return allVotes{}, fmt.Errorf("context done: %w", ctx.Err())
		}

		// note that votes after this call will _not_ be counted towards our votes
		// for this round, as the late votes can be cast after the weak coin is revealed. we
		// count them towards our votes in the next round.
		ownVotes, undecided = pd.calcVotesBeforeWeakCoin(rLogger, st)

		timer.Reset(pd.config.WeakCoinRoundDuration)

		pd.eg.Go(func() error {
			participants := make([]weakcoin.Participant, 0, len(st.active))
			for _, session := range st.active {
				participants = append(participants, weakcoin.Participant{
					Signer: session.signer.VRFSigner(),
					Nonce:  session.nonce,
				})
			}
			pd.weakCoin.StartRound(ctx, round, participants)
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
			rLogger.Error("failed to generate weak coin", zap.Error(err))
			return allVotes{}, err
		}

		tallyUndecided(&ownVotes, undecided, flip)
	}

	logger.Debug("consensus phase finished")
	return ownVotes, nil
}

func (pd *ProtocolDriver) markProposalPhaseFinished(st *state, finishedAt time.Time) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	st.proposalPhaseFinishedTime = finishedAt
}

func (pd *ProtocolDriver) calcVotesBeforeWeakCoin(logger *zap.Logger, st *state) (allVotes, proposalList) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return calcVotes(logger, pd.theta, st)
}

func (pd *ProtocolDriver) sendFirstRoundVote(
	ctx context.Context,
	msg FirstVotingMessageBody,
	signer *signing.EdSigner,
) error {
	m := FirstVotingMessage{
		FirstVotingMessageBody: msg,
		SmesherID:              signer.NodeID(),
		Signature:              signer.Sign(signing.BEACON_FIRST_MSG, codec.MustEncode(&msg)),
	}

	pd.logger.Debug("sending first round vote",
		log.ZContext(ctx),
		zap.Uint32("epoch", msg.EpochID.Uint32()),
		zap.Uint32("round", uint32(types.FirstRound)),
		log.ZShortStringer("node_id", signer.NodeID()),
	)
	return pd.sendToGossip(ctx, pubsub.BeaconFirstVotesProtocol, codec.MustEncode(&m))
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

func (pd *ProtocolDriver) sendFollowingVote(
	ctx context.Context,
	epoch types.EpochID,
	round types.RoundID,
	ownCurrentRoundVotes allVotes,
	signer *signing.EdSigner,
) error {
	firstRoundVotes, err := pd.getFirstRoundVote(epoch, signer.NodeID())
	if err != nil {
		return fmt.Errorf("get own first round votes %s: %w", signer.NodeID(), err)
	}

	bitVector := encodeVotes(ownCurrentRoundVotes, firstRoundVotes)
	mb := FollowingVotingMessageBody{
		EpochID:        epoch,
		RoundID:        round,
		VotesBitVector: bitVector,
	}

	m := FollowingVotingMessage{
		FollowingVotingMessageBody: mb,
		SmesherID:                  signer.NodeID(),
		Signature:                  signer.Sign(signing.BEACON_FOLLOWUP_MSG, codec.MustEncode(&mb)),
	}

	pd.logger.Debug("sending following round vote",
		log.ZContext(ctx),
		zap.Uint32("epoch", epoch.Uint32()),
		zap.Uint32("round", uint32(round)),
		log.ZShortStringer("node_id", signer.NodeID()),
	)
	return pd.sendToGossip(ctx, pubsub.BeaconFollowingVotesProtocol, codec.MustEncode(&m))
}

type proposalChecker struct {
	threshold       *big.Int
	thresholdStrict *big.Int
}

func createProposalChecker(logger *zap.Logger, conf Config, numEarlyATXs, numATXs int) eligibilityChecker {
	if numATXs == 0 {
		logger.Error("creating proposal checker with zero atxs")
		return &proposalChecker{threshold: big.NewInt(0), thresholdStrict: big.NewInt(0)}
	}

	high := atxThreshold(conf.Kappa, &conf.Q, numEarlyATXs)
	low := atxThreshold(conf.Kappa, &conf.Q, numATXs)
	logger.Debug("created proposal checker with ATX threshold",
		zap.Int("num_early_atxs", numEarlyATXs),
		zap.Int("num_atxs", numATXs),
		zap.Stringer("threshold", high),
		zap.Stringer("threshold_strict", low),
	)
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
		sigLengthBits = types.VrfSignatureSize * 8
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

func buildSignedProposal(
	ctx context.Context,
	logger *zap.Logger,
	signer vrfSigner,
	epoch types.EpochID,
	nonce types.VRFPostIndex,
) types.VrfSignature {
	p := buildProposal(epoch, nonce)
	vrfSig := signer.Sign(p)
	proposal := ProposalFromVrf(vrfSig)
	logger.Debug("calculated beacon proposal",
		log.ZContext(ctx),
		zap.Uint32("epoch", epoch.Uint32()),
		zap.Uint64("vrf_nonce", uint64(nonce)),
		zap.Inline(proposal),
		log.ZShortStringer("node_id", signer.NodeID()),
	)
	return vrfSig
}

func buildProposal(epoch types.EpochID, nonce types.VRFPostIndex) []byte {
	message := &ProposalVrfMessage{
		Type:  types.EligibilityBeacon,
		Nonce: nonce,
		Epoch: epoch,
	}
	return codec.MustEncode(message)
}

func (pd *ProtocolDriver) sendToGossip(ctx context.Context, protocol string, serialized []byte) error {
	if err := pd.publisher.Publish(ctx, protocol, serialized); err != nil {
		return fmt.Errorf("publishing on protocol %s: %w", protocol, err)
	}
	return nil
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
				Beacon:     beacon.String(),
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
			Beacon: beacon.String(),
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
	return mt.firstVoteSendTime(epoch).
		Add(mt.conf.FirstVotingRoundDuration).
		Add(subsequentRoundDuration * time.Duration(round-1))
}

// WeakCoinProposalSendTime returns the time at which the weak coin proposals are sent for an epoch and round.
func (mt *messageTimes) WeakCoinProposalSendTime(epoch types.EpochID, round types.RoundID) time.Time {
	subsequentRoundDuration := mt.conf.VotingRoundDuration + mt.conf.WeakCoinRoundDuration
	return mt.firstVoteSendTime(epoch).
		Add(mt.conf.FirstVotingRoundDuration + mt.conf.VotingRoundDuration).
		Add(subsequentRoundDuration * time.Duration(round-1))
}
