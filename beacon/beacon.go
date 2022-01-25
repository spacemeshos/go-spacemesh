package beacon

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ALTree/bigfloat"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/beacon/metrics"
	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

const (
	protoName      = "BEACON_PROTOCOL"
	proposalPrefix = "BP"

	proposalChanCapacity = 1024
	numEpochsToKeep      = 10
)

var (
	errBeaconNotCalculated = errors.New("beacon is not calculated for this epoch")
	errZeroEpochWeight     = errors.New("zero epoch weight provided")
)

type (
	proposals    = struct{ valid, potentiallyValid [][]byte }
	allVotes     = struct{ valid, invalid proposalSet }
	ballotWeight = struct {
		weight  uint64
		ballots map[types.BallotID]struct{}
	}
)

type layerClock interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timesync.LayerTimer)
	LayerToTime(types.LayerID) time.Time
}

// New returns a new ProtocolDriver.
func New(
	conf Config,
	nodeID types.NodeID,
	publisher pubsub.Publisher,
	atxDB activationDB,
	edSigner signing.Signer,
	edVerifier signing.VerifyExtractor,
	vrfSigner signing.Signer,
	vrfVerifier signing.Verifier,
	weakCoin coin,
	db database.Database,
	clock layerClock,
	logger log.Log,
) *ProtocolDriver {
	pd := &ProtocolDriver{
		logger:                  logger,
		config:                  conf,
		nodeID:                  nodeID,
		theta:                   new(big.Float).SetRat(conf.Theta),
		publisher:               publisher,
		atxDB:                   atxDB,
		edSigner:                edSigner,
		edVerifier:              edVerifier,
		vrfSigner:               vrfSigner,
		vrfVerifier:             vrfVerifier,
		weakCoin:                weakCoin,
		db:                      db,
		clock:                   clock,
		beacons:                 make(map[types.EpochID]types.Beacon),
		beaconsFromBallots:      make(map[types.EpochID]map[types.Beacon]*ballotWeight),
		hasProposed:             make(map[string]struct{}),
		hasVoted:                make([]map[string]struct{}, conf.RoundsNumber),
		firstRoundIncomingVotes: make(map[string][][]byte),
		proposalChans:           make(map[types.EpochID]chan *proposalMessageWithReceiptData),
		votesMargin:             map[string]*big.Int{},
	}
	pd.metricsCollector = metrics.NewBeaconMetricsCollector(pd.gatherMetricsData, logger.WithName("metrics"))
	return pd
}

// ProtocolDriver is the driver for the beacon protocol.
type ProtocolDriver struct {
	running    uint64
	inProtocol uint64
	eg         errgroup.Group
	cancel     context.CancelFunc

	logger log.Log

	config      Config
	nodeID      types.NodeID
	sync        system.SyncStateProvider
	publisher   pubsub.Publisher
	atxDB       activationDB
	edSigner    signing.Signer
	edVerifier  signing.VerifyExtractor
	vrfSigner   signing.Signer
	vrfVerifier signing.Verifier
	weakCoin    coin
	theta       *big.Float

	clock       layerClock
	layerTicker chan types.LayerID
	db          database.Database

	mu              sync.RWMutex
	lastTickedEpoch types.EpochID
	epochInProgress types.EpochID
	epochWeight     uint64

	// beacons store calculated beacons as the result of the beacon protocol.
	// the map key is the target epoch when beacon is used. if a beacon is calculated in epoch N, it will be used
	// in epoch N+1.
	beacons map[types.EpochID]types.Beacon
	// beaconsFromBallots store beacons collected from ballots.
	// the map key is the epoch when the ballot is published. the beacon value is calculated in the
	// previous epoch and used in the current epoch.
	beaconsFromBallots map[types.EpochID]map[types.Beacon]*ballotWeight

	// the original proposals as received, bucketed by validity.
	incomingProposals proposals
	// minerPublicKey -> list of proposal.
	// this list is used in encoding/decoding votes for each miner in all subsequent voting rounds.
	firstRoundIncomingVotes map[string][][]byte
	// TODO(nkryuchkov): For every round excluding first round consider having a vector of opinions.
	votesMargin               map[string]*big.Int
	hasProposed               map[string]struct{}
	hasVoted                  []map[string]struct{}
	proposalPhaseFinishedTime time.Time
	proposalChans             map[types.EpochID]chan *proposalMessageWithReceiptData
	proposalChecker           eligibilityChecker

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
func (pd *ProtocolDriver) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapUint64(&pd.running, 0, 1) {
		pd.logger.WithContext(ctx).Warning("attempt to start beacon protocol more than once")
		return nil
	}
	pd.logger.Info("starting %v with the following config: %+v", protoName, pd.config)
	if pd.sync == nil {
		pd.logger.Panic("update sync state provider can't be nil")
	}

	ctx, cancel := context.WithCancel(ctx)
	pd.cancel = cancel

	pd.layerTicker = pd.clock.Subscribe()
	pd.metricsCollector.Start(nil)

	pd.eg.Go(func() error {
		pd.listenLayers(ctx)
		return fmt.Errorf("context error: %w", ctx.Err())
	})

	return nil
}

// Close closes ProtocolDriver.
func (pd *ProtocolDriver) Close() {
	if !atomic.CompareAndSwapUint64(&pd.running, 1, 0) {
		return
	}
	pd.logger.Info("closing %v", protoName)
	pd.cancel()
	pd.metricsCollector.Stop()
	pd.clock.Unsubscribe(pd.layerTicker)
	pd.logger.Info("waiting for beacon goroutines to finish")
	if err := pd.eg.Wait(); err != nil {
		pd.logger.With().Info("received error waiting for goroutines to finish", log.Err(err))
	}
	pd.logger.Info("beacon goroutines finished")
}

// isClosed returns true if background workers are not running.
func (pd *ProtocolDriver) isClosed() bool {
	return atomic.LoadUint64(&pd.running) == 0
}

// ReportBeaconFromBallot reports the beacon value in a ballot along with the smesher's weight unit.
func (pd *ProtocolDriver) ReportBeaconFromBallot(epoch types.EpochID, bid types.BallotID, beacon types.Beacon, weight uint64) {
	pd.recordBeacon(epoch, bid, beacon, weight)

	if _, err := pd.GetBeacon(epoch); err == nil {
		// already has beacon. i.e. we had participated in the beacon protocol during the last epoch
		return
	}

	if eBeacon := pd.findMostWeightedBeaconForEpoch(epoch); eBeacon != types.EmptyBeacon {
		pd.setBeacon(epoch, eBeacon)
	}
}

func (pd *ProtocolDriver) recordBeacon(epochID types.EpochID, bid types.BallotID, beacon types.Beacon, weight uint64) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if _, ok := pd.beaconsFromBallots[epochID]; !ok {
		pd.beaconsFromBallots[epochID] = make(map[types.Beacon]*ballotWeight)
	}
	entry, ok := pd.beaconsFromBallots[epochID][beacon]
	if !ok {
		pd.beaconsFromBallots[epochID][beacon] = &ballotWeight{
			weight:  weight,
			ballots: map[types.BallotID]struct{}{bid: {}},
		}
		pd.logger.With().Debug("added beacon from ballot",
			epochID,
			bid,
			beacon,
			log.Uint64("weight", weight))
		return
	}

	// checks if we have recorded this blockID before
	if _, ok := entry.ballots[bid]; ok {
		pd.logger.With().Warning("ballot already reported beacon", epochID, bid)
		return
	}

	entry.ballots[bid] = struct{}{}
	entry.weight += weight
	pd.logger.With().Debug("added beacon from ballot",
		epochID,
		bid,
		beacon,
		log.Uint64("weight", weight))
}

func (pd *ProtocolDriver) findMostWeightedBeaconForEpoch(epoch types.EpochID) types.Beacon {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	epochBeacons, ok := pd.beaconsFromBallots[epoch]
	if !ok {
		return types.EmptyBeacon
	}
	var mostWeight uint64
	var beacon types.Beacon
	numBallots := 0
	for k, v := range epochBeacons {
		if v.weight > mostWeight {
			beacon = k
			mostWeight = v.weight
		}
		numBallots += len(v.ballots)
	}

	logger := pd.logger.WithFields(epoch, log.Int("num_ballots", numBallots))

	if uint32(numBallots) < pd.config.BeaconSyncNumBallots {
		logger.Debug("not enough ballots to determine beacon")
		return types.EmptyBeacon
	}

	logger.With().Info("beacon determined for epoch",
		beacon,
		log.Uint64("weight", mostWeight))
	return beacon
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

	if errors.Is(err, database.ErrNotFound) {
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
	pd.beacons[targetEpoch] = beacon
	return nil
}

func (pd *ProtocolDriver) persistBeacon(epoch types.EpochID, beacon types.Beacon) error {
	if err := pd.db.Put(epoch.ToBytes(), beacon.Bytes()); err != nil {
		pd.logger.With().Error("failed to persist beacon", epoch, beacon, log.Err(err))
		return fmt.Errorf("persist beacon: %w", err)
	}

	return nil
}

func (pd *ProtocolDriver) getPersistedBeacon(epoch types.EpochID) (types.Beacon, error) {
	data, err := pd.db.Get(epoch.ToBytes())
	if err != nil {
		return types.EmptyBeacon, fmt.Errorf("get from DB: %w", err)
	}

	return types.BytesToBeacon(data), nil
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

func (pd *ProtocolDriver) setupEpoch(epoch types.EpochID, epochWeight uint64, logger log.Log) chan *proposalMessageWithReceiptData {
	pd.cleanupEpoch(epoch - 1) // just in case we processed any gossip messages before the protocol started

	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.epochWeight = epochWeight
	pd.proposalChecker = createProposalChecker(pd.config.Kappa, pd.config.Q, epochWeight, logger)
	ch := pd.getOrCreateProposalChannel(epoch)
	// allow proposals for the next epoch to come in early
	_ = pd.getOrCreateProposalChannel(epoch + 1)
	return ch
}

func (pd *ProtocolDriver) cleanupEpoch(epoch types.EpochID) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	pd.incomingProposals = proposals{}
	pd.firstRoundIncomingVotes = map[string][][]byte{}
	pd.votesMargin = map[string]*big.Int{}
	pd.hasProposed = make(map[string]struct{})
	pd.hasVoted = make([]map[string]struct{}, pd.config.RoundsNumber)
	pd.proposalPhaseFinishedTime = time.Time{}

	if ch, ok := pd.proposalChans[epoch]; ok {
		close(ch)
		delete(pd.proposalChans, epoch)
	}

	if epoch <= numEpochsToKeep {
		return
	}
	oldest := epoch - numEpochsToKeep
	if _, ok := pd.beacons[oldest]; ok {
		delete(pd.beacons, oldest)
	}
	if _, ok := pd.beaconsFromBallots[oldest]; ok {
		delete(pd.beaconsFromBallots, oldest)
	}
}

// listens to new layers.
func (pd *ProtocolDriver) listenLayers(ctx context.Context) {
	pd.logger.With().Info("starting listening layers")

	for {
		select {
		case <-ctx.Done():
			return
		case layer := <-pd.layerTicker:
			pd.logger.With().Debug("received tick", layer)
			pd.eg.Go(func() error {
				pd.handleLayer(ctx, layer)
				return nil
			})
		}
	}
}

func (pd *ProtocolDriver) setTickedEpoch(epoch types.EpochID) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.lastTickedEpoch = epoch
}

func (pd *ProtocolDriver) setEpochInProgress(epoch types.EpochID) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.epochInProgress = epoch
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new CPs.
func (pd *ProtocolDriver) handleLayer(ctx context.Context, layer types.LayerID) {
	epoch := layer.GetEpoch()
	logger := pd.logger.WithContext(ctx).WithFields(layer, epoch)

	pd.setTickedEpoch(epoch)
	if !layer.FirstInEpoch() {
		logger.Debug("not first layer in epoch, skipping")
		return
	}
	if pd.isInProtocol() {
		logger.Error("last beacon protocol is still running, skipping")
		return
	}
	logger.Info("first layer in epoch, proceeding")
	pd.handleEpoch(ctx, epoch)
}

func (pd *ProtocolDriver) handleEpoch(ctx context.Context, epoch types.EpochID) {
	ctx = log.WithNewSessionID(ctx)
	targetEpoch := epoch + 1
	logger := pd.logger.WithContext(ctx).WithFields(epoch, log.Uint32("target_epoch", uint32(targetEpoch)))
	// TODO(nkryuchkov): check when epoch started, adjust waiting time for this timestamp
	if epoch.IsGenesis() {
		logger.Debug("not starting beacon protocol since we are in genesis epoch")
		return
	}
	if !pd.sync.IsSynced(ctx) {
		logger.Info("beacon protocol is skipped while node is not synced")
		return
	}

	// make sure this node has ATX in the last epoch and is eligible to participate in the beacon protocol
	atxID, err := pd.atxDB.GetNodeAtxIDForEpoch(pd.nodeID, epoch-1)
	if err != nil {
		logger.With().Info("node has no ATX in last epoch, not participating in beacon protocol", log.Err(err))
		return
	}

	logger.With().Info("participating in beacon protocol with ATX", atxID)

	epochWeight, atxs, err := pd.atxDB.GetEpochWeight(epoch)
	if err != nil {
		logger.With().Error("failed to get weight targeting epoch", log.Err(err))
		return
	}
	if epochWeight == 0 {
		logger.With().Error("zero weight targeting epoch", log.Err(errZeroEpochWeight))
		return
	}

	pd.setEpochInProgress(epoch)
	pd.setBeginProtocol(ctx)
	defer pd.setEndProtocol(ctx)

	ch := pd.setupEpoch(epoch, epochWeight, logger)
	defer pd.cleanupEpoch(epoch)

	pd.eg.Go(func() error {
		pd.readProposalMessagesLoop(ctx, ch)
		return nil
	})

	pd.startWeakCoinEpoch(ctx, epoch, atxs)
	defer pd.weakCoin.FinishEpoch(ctx, epoch)

	pd.runProposalPhase(ctx, epoch)
	lastRoundOwnVotes, err := pd.runConsensusPhase(ctx, epoch)
	if err != nil {
		logger.With().Warning("consensus execution canceled", log.Err(err))
		return
	}

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	beacon := calcBeacon(logger, lastRoundOwnVotes)
	events.ReportCalculatedBeacon(targetEpoch, beacon.ShortString())

	if err := pd.setBeacon(targetEpoch, beacon); err != nil {
		logger.With().Error("failed to set beacon", log.Err(err))
		return
	}

	logger.With().Info("beacon set for epoch", beacon)
}

func calcBeacon(logger log.Log, lastRoundVotes allVotes) types.Beacon {
	logger.Info("calculating beacon")

	allHashes := lastRoundVotes.valid.sort()
	allHashHexes := make([]string, len(allHashes))
	for i, h := range allHashes {
		allHashHexes[i] = types.BytesToHash([]byte(h)).ShortString()
	}
	logger.With().Debug("calculating beacon from this hash list",
		log.String("hashes", strings.Join(allHashHexes, ", ")))

	beacon := types.Beacon(allHashes.hash())
	logger.With().Info("calculated beacon", beacon, log.Int("num_hashes", len(allHashes)))

	return beacon
}

func (pd *ProtocolDriver) getOrCreateProposalChannel(epoch types.EpochID) chan *proposalMessageWithReceiptData {
	ch, ok := pd.proposalChans[epoch]
	if !ok {
		ch = make(chan *proposalMessageWithReceiptData, proposalChanCapacity)
		pd.proposalChans[epoch] = ch
	}

	return ch
}

func (pd *ProtocolDriver) runProposalPhase(ctx context.Context, epoch types.EpochID) {
	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	logger.Debug("starting proposal phase")

	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, pd.config.ProposalDuration)
	defer cancel()

	pd.eg.Go(func() error {
		logger.Debug("starting proposal message sender")

		if err := pd.proposalPhaseImpl(ctx, epoch); err != nil {
			logger.With().Error("failed to send proposal message", log.Err(err))
		}

		logger.Debug("proposal message sender finished")
		return nil
	})

	<-ctx.Done()

	pd.markProposalPhaseFinished(epoch)

	logger.Debug("proposal phase finished")
}

func (pd *ProtocolDriver) proposalPhaseImpl(ctx context.Context, epoch types.EpochID) error {
	if pd.isClosed() {
		return nil
	}

	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	proposedSignature := buildSignedProposal(ctx, pd.vrfSigner, epoch, pd.logger)

	pd.mu.RLock()
	logger.With().Debug("calculated proposal signature",
		log.String("signature", string(proposedSignature)),
		log.Uint64("total_weight", pd.epochWeight))

	passes := pd.proposalChecker.IsProposalEligible(proposedSignature)
	pd.mu.RUnlock()

	if !passes {
		logger.With().Debug("proposal to be sent doesn't pass threshold",
			log.String("proposal", string(proposedSignature)))
		// proposal is not sent
		return nil
	}

	logger.With().Debug("Proposal to be sent passes threshold",
		log.String("proposal", string(proposedSignature)))

	// concat them into a single proposal message
	m := ProposalMessage{
		EpochID:      epoch,
		NodeID:       pd.nodeID,
		VRFSignature: proposedSignature,
	}

	logger.With().Debug("going to send proposal", log.String("message", m.String()))

	if err := pd.sendToGossip(ctx, ProposalProtocol, m); err != nil {
		return fmt.Errorf("broadcast proposal message: %w", err)
	}

	logger.With().Info("sent proposal", log.String("message", m.String()))
	return nil
}

func (pd *ProtocolDriver) getProposalChannel(ctx context.Context, epoch types.EpochID) chan *proposalMessageWithReceiptData {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	logger := pd.logger.WithContext(ctx).WithFields(
		log.FieldNamed("current_epoch", pd.epochInProgress),
		log.FieldNamed("proposal_epoch", epoch))
	switch {
	case epoch < pd.epochInProgress:
		logger.With().Debug("proposal too old, do not accept")
		return nil
	case epoch == pd.epochInProgress:
		ongoing := pd.proposalPhaseFinishedTime == time.Time{}
		if ongoing {
			return pd.getOrCreateProposalChannel(epoch)
		}
		logger.With().Debug("proposal phase ended, do not accept")
		return nil
	case epoch == pd.epochInProgress+1:
		// always accept proposals for the next epoch, but not too far in the future
		logger.Debug("accepting proposal for the next epoch")
		ch := pd.getOrCreateProposalChannel(epoch)
		if len(ch) == proposalChanCapacity {
			// the reader loop is not started for the next epoch yet. drop old messages if it's already full
			// channel receive is not synchronous with length check, use select+default here to prevent potential blocking
			select {
			case msg := <-ch:
				logger.With().Warning("proposal channel for next epoch is full, dropping oldest msg",
					log.String("message", msg.message.String()))
			default:
			}
		}
		return ch
	default:
		return nil
	}
}

// runConsensusPhase runs K voting rounds and returns result from last weak coin round.
func (pd *ProtocolDriver) runConsensusPhase(ctx context.Context, epoch types.EpochID) (allVotes, error) {
	logger := pd.logger.WithContext(ctx).WithFields(epoch)
	logger.Debug("starting consensus phase")

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
		rLogger := logger.WithFields(round)
		votes := ownVotes
		pd.eg.Go(func() error {
			if round == types.FirstRound {
				if err := pd.sendProposalVote(ctx, epoch); err != nil {
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
		ownVotes, undecided, err = pd.calcVotes(ctx, epoch, round)
		if err != nil {
			logger.With().Error("failed to calculate votes", log.Err(err))
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

	logger.Debug("consensus phase finished")
	return ownVotes, nil
}

func (pd *ProtocolDriver) startWeakCoinEpoch(ctx context.Context, epoch types.EpochID, atxs []types.ATXID) {
	// we need to pass a map with spacetime unit allowances before any round is started
	ua := weakcoin.UnitAllowances{}
	for _, id := range atxs {
		header, err := pd.atxDB.GetAtxHeader(id)
		if err != nil {
			pd.logger.WithContext(ctx).With().Panic("unable to load atx header", log.Err(err))
		}
		ua[string(header.NodeID.VRFPublicKey)] += uint64(header.NumUnits)
	}

	pd.weakCoin.StartEpoch(ctx, epoch, ua)
}

func (pd *ProtocolDriver) markProposalPhaseFinished(epoch types.EpochID) {
	finishedAt := time.Now()
	pd.mu.Lock()
	pd.proposalPhaseFinishedTime = finishedAt
	pd.mu.Unlock()
	pd.logger.Debug("marked proposal phase for epoch %v finished at %v", epoch, finishedAt.String())
}

func (pd *ProtocolDriver) receivedBeforeProposalPhaseFinished(epoch types.EpochID, receivedAt time.Time) bool {
	pd.mu.RLock()
	finishedAt := pd.proposalPhaseFinishedTime
	pd.mu.RUnlock()
	hasFinished := !finishedAt.IsZero()

	pd.logger.Debug("checking if timestamp %v was received before proposal phase finished in epoch %v, is phase finished: %v, finished at: %v", receivedAt.String(), epoch, hasFinished, finishedAt.String())

	return !hasFinished || receivedAt.Before(finishedAt)
}

func (pd *ProtocolDriver) sendProposalVote(ctx context.Context, epoch types.EpochID) error {
	// round 1, send hashed proposal
	// create a voting message that references all seen proposals within δ time frame and send it

	// TODO(nkryuchkov): also send a bit vector
	// TODO(nkryuchkov): initialize margin vector to initial votes
	// TODO(nkryuchkov): use weight

	pd.mu.RLock()
	defer pd.mu.RUnlock()

	return pd.sendFirstRoundVote(ctx, epoch, pd.incomingProposals)
}

func (pd *ProtocolDriver) sendFirstRoundVote(ctx context.Context, epoch types.EpochID, proposals proposals) error {
	mb := FirstVotingMessageBody{
		EpochID:                   epoch,
		ValidProposals:            proposals.valid,
		PotentiallyValidProposals: proposals.potentiallyValid,
	}

	sig := signMessage(pd.edSigner, mb, pd.logger)

	m := FirstVotingMessage{
		FirstVotingMessageBody: mb,
		Signature:              sig,
	}

	pd.logger.WithContext(ctx).With().Debug("sending first round vote",
		epoch,
		types.FirstRound,
		log.String("message", m.String()))

	if err := pd.sendToGossip(ctx, FirstVoteProtocol, m); err != nil {
		return fmt.Errorf("sendToGossip: %w", err)
	}

	return nil
}

func (pd *ProtocolDriver) sendFollowingVote(ctx context.Context, epoch types.EpochID, round types.RoundID, ownCurrentRoundVotes allVotes) error {
	pd.mu.RLock()
	bitVector := encodeVotes(ownCurrentRoundVotes, pd.firstRoundIncomingVotes[string(pd.edSigner.PublicKey().Bytes())])
	pd.mu.RUnlock()

	mb := FollowingVotingMessageBody{
		EpochID:        epoch,
		RoundID:        round,
		VotesBitVector: bitVector,
	}

	sig := signMessage(pd.edSigner, mb, pd.logger)

	m := FollowingVotingMessage{
		FollowingVotingMessageBody: mb,
		Signature:                  sig,
	}

	pd.logger.WithContext(ctx).With().Debug("sending following round vote",
		epoch,
		round,
		log.String("message", m.String()))

	if err := pd.sendToGossip(ctx, FollowingVotingProtocol, m); err != nil {
		return fmt.Errorf("broadcast voting message: %w", err)
	}

	return nil
}

func (pd *ProtocolDriver) votingThreshold(epochWeight uint64) *big.Int {
	v, _ := new(big.Float).Mul(
		pd.theta,
		new(big.Float).SetUint64(epochWeight),
	).Int(nil)

	return v
}

type proposalChecker struct {
	threshold *big.Int
}

func createProposalChecker(kappa uint64, q *big.Rat, epochWeight uint64, logger log.Log) *proposalChecker {
	if epochWeight == 0 {
		logger.Panic("creating proposal checker with zero weight")
	}

	threshold := atxThreshold(kappa, q, epochWeight)
	logger.With().Info("created proposal checker with ATX threshold",
		log.Uint64("epoch_weight", epochWeight),
		log.String("threshold", threshold.String()))
	return &proposalChecker{threshold: threshold}
}

func (pc *proposalChecker) IsProposalEligible(proposal []byte) bool {
	proposalInt := new(big.Int).SetBytes(proposal)
	return proposalInt.Cmp(pc.threshold) == -1
}

// TODO(nkryuchkov): Consider replacing github.com/ALTree/bigfloat.
func atxThresholdFraction(kappa uint64, q *big.Rat, epochWeight uint64) *big.Float {
	if epochWeight == 0 {
		return big.NewFloat(0)
	}

	// threshold(k, q, W) = 1 - (2 ^ (- (k/((1-q)*W))))
	// Floating point: 1 - math.Pow(2.0, -(float64(tb.config.Kappa)/((1.0-tb.config.Q)*float64(epochWeight))))
	// Fixed point:
	v := new(big.Float).Sub(
		new(big.Float).SetInt64(1),
		bigfloat.Pow(
			new(big.Float).SetInt64(2),
			new(big.Float).SetRat(
				new(big.Rat).Neg(
					new(big.Rat).Quo(
						new(big.Rat).SetUint64(kappa),
						new(big.Rat).Mul(
							new(big.Rat).Sub(
								new(big.Rat).SetInt64(1.0),
								q,
							),
							new(big.Rat).SetUint64(epochWeight),
						),
					),
				),
			),
		),
	)

	return v
}

// TODO(nkryuchkov): Consider having a generic function for probabilities.
func atxThreshold(kappa uint64, q *big.Rat, epochWeight uint64) *big.Int {
	const signatureLength = 64 * 8

	fraction := atxThresholdFraction(kappa, q, epochWeight)
	two := big.NewInt(2)
	signatureLengthBigInt := big.NewInt(signatureLength)

	maxPossibleNumberBigInt := new(big.Int).Exp(two, signatureLengthBigInt, nil)
	maxPossibleNumberBigFloat := new(big.Float).SetInt(maxPossibleNumberBigInt)

	thresholdBigFloat := new(big.Float).Mul(maxPossibleNumberBigFloat, fraction)
	threshold, _ := thresholdBigFloat.Int(nil)

	return threshold
}

func buildSignedProposal(ctx context.Context, signer signing.Signer, epoch types.EpochID, logger log.Log) []byte {
	p := buildProposal(epoch, logger)
	signature := signer.Sign(p)
	logger.WithContext(ctx).With().Debug("calculated signature",
		epoch,
		log.String("proposal", util.Bytes2Hex(p)),
		log.String("signature", string(signature)))

	return signature
}

func signMessage(signer signing.Signer, message interface{}, logger log.Log) []byte {
	encoded, err := types.InterfaceToBytes(message)
	if err != nil {
		logger.With().Panic("failed to serialize message for signing", log.Err(err))
	}
	return signer.Sign(encoded)
}

func buildProposal(epoch types.EpochID, logger log.Log) []byte {
	message := &struct {
		Prefix string
		Epoch  uint32
	}{
		Prefix: proposalPrefix,
		Epoch:  uint32(epoch),
	}

	b, err := types.InterfaceToBytes(message)
	if err != nil {
		logger.With().Panic("failed to serialize proposal", log.Err(err))
	}
	return b
}

func (pd *ProtocolDriver) sendToGossip(ctx context.Context, protocol string, data interface{}) error {
	serialized, err := types.InterfaceToBytes(data)
	if err != nil {
		pd.logger.With().Panic("failed to serialize message for gossip", log.Err(err))
	}

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
	return nil
}

func (pd *ProtocolDriver) getOwnWeight(epoch types.EpochID) uint64 {
	atxID, err := pd.atxDB.GetNodeAtxIDForEpoch(pd.nodeID, epoch)
	if err != nil {
		pd.logger.With().Error("failed to look up own ATX for epoch", epoch, log.Err(err))
		return 0
	}
	hdr, err := pd.atxDB.GetAtxHeader(atxID)
	if err != nil {
		pd.logger.With().Error("failed to look up own weight for epoch", epoch, log.Err(err))
		return 0
	}
	return hdr.GetWeight()
}

func (pd *ProtocolDriver) gatherMetricsData() ([]*metrics.BeaconStats, *metrics.BeaconStats) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	epoch := pd.lastTickedEpoch
	var observed []*metrics.BeaconStats
	if beacons, ok := pd.beaconsFromBallots[epoch]; ok {
		for beacon, stats := range beacons {
			stat := &metrics.BeaconStats{
				Epoch:  epoch,
				Beacon: beacon.ShortString(),
				Count:  uint64(len(stats.ballots)),
				Weight: stats.weight,
			}
			observed = append(observed, stat)
		}
	}
	var calculated *metrics.BeaconStats
	ownWeight := uint64(0)
	if !epoch.IsGenesis() {
		ownWeight = pd.getOwnWeight(epoch - 1)
	}
	// whether the beacon for the next epoch is calculated
	if beacon, ok := pd.beacons[epoch+1]; ok {
		calculated = &metrics.BeaconStats{
			Epoch:  epoch + 1,
			Beacon: beacon.ShortString(),
			Count:  1,
			Weight: ownWeight,
		}
	}

	return observed, calculated
}
