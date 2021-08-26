package tortoisebeacon

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ALTree/bigfloat"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/taskgroup"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
)

const (
	protoName            = "TORTOISE_BEACON_PROTOCOL"
	proposalPrefix       = "TBP"
	firstRound           = types.RoundID(0)
	genesisBeacon        = "0xaeebad4a796fcc2e15dc4c6061b45ed9b373f26adfc798ca7d2d8cc58182718e" // sha256("genesis")
	proposalChanCapacity = 1024
)

// Tortoise Beacon errors.
var (
	ErrBeaconNotCalculated = errors.New("beacon is not calculated for this epoch")
	ErrZeroEpochWeight     = errors.New("zero epoch weight provided")
	ErrZeroEpoch           = errors.New("zero epoch provided")
)

type broadcaster interface {
	Broadcast(ctx context.Context, channel string, data []byte) error
}

type tortoiseBeaconDB interface {
	GetTortoiseBeacon(epochID types.EpochID) (types.Hash32, error)
	SetTortoiseBeacon(epochID types.EpochID, beacon types.Hash32) error
}

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./weak_coin.go coin

type coin interface {
	StartEpoch(types.EpochID, weakcoin.UnitAllowances)
	StartRound(context.Context, types.RoundID) error
	FinishRound()
	Get(types.EpochID, types.RoundID) bool
	FinishEpoch()
	HandleSerializedMessage(context.Context, service.GossipMessage, service.Fetcher)
}

type (
	proposals = struct{ valid, potentiallyValid [][]byte }
	allVotes  = struct{ valid, invalid proposalSet }
)

type layerClock interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timesync.LayerTimer)
	AwaitLayer(types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

// SyncState interface to check the state the sync.
type SyncState interface {
	IsSynced(context.Context) bool
}

// New returns a new TortoiseBeacon.
func New(
	conf Config,
	layerDuration time.Duration,
	nodeID types.NodeID,
	net broadcaster,
	atxDB activationDB,
	tortoiseBeaconDB tortoiseBeaconDB,
	edSigner signing.Signer,
	edVerifier signing.VerifyExtractor,
	vrfSigner signing.Signer,
	vrfVerifier signing.Verifier,
	weakCoin coin,
	clock layerClock,
	logger log.Log,
) *TortoiseBeacon {
	return &TortoiseBeacon{
		Log:                     logger,
		config:                  conf,
		layerDuration:           layerDuration,
		nodeID:                  nodeID,
		net:                     net,
		atxDB:                   atxDB,
		tortoiseBeaconDB:        tortoiseBeaconDB,
		edSigner:                edSigner,
		edVerifier:              edVerifier,
		vrfSigner:               vrfSigner,
		vrfVerifier:             vrfVerifier,
		weakCoin:                weakCoin,
		clock:                   clock,
		beacons:                 make(map[types.EpochID]types.Hash32),
		hasVoted:                make([]map[string]struct{}, conf.RoundsNumber),
		firstRoundIncomingVotes: make(map[string]proposals),
		seenEpochs:              make(map[types.EpochID]struct{}),
		proposalChans:           make(map[types.EpochID]chan *proposalMessageWithReceiptData),
		votesMargin:             map[string]*big.Int{},
	}
}

// TortoiseBeacon represents Tortoise Beacon.
type TortoiseBeacon struct {
	closed uint64
	tg     *taskgroup.Group
	cancel context.CancelFunc

	log.Log

	config        Config
	layerDuration time.Duration
	nodeID        types.NodeID

	sync             SyncState
	net              broadcaster
	atxDB            activationDB
	tortoiseBeaconDB tortoiseBeaconDB
	edSigner         signing.Signer
	edVerifier       signing.VerifyExtractor
	vrfSigner        signing.Signer
	vrfVerifier      signing.Verifier
	weakCoin         coin

	seenEpochsMu sync.Mutex
	seenEpochs   map[types.EpochID]struct{}

	clock       layerClock
	layerTicker chan types.LayerID
	layerMu     sync.RWMutex
	lastLayer   types.LayerID

	consensusMu sync.RWMutex
	// TODO(nkryuchkov): have a mixed list of all sorted proposals
	// have one bit vector: valid proposals
	incomingProposals       proposals
	firstRoundIncomingVotes map[string]proposals // sorted votes for bit vector decoding
	// TODO(nkryuchkov): For every round excluding first round consider having a vector of opinions.
	votesMargin map[string]*big.Int
	hasVoted    []map[string]struct{}

	proposalPhaseFinishedTimeMu sync.RWMutex
	proposalPhaseFinishedTime   time.Time

	beaconsMu sync.RWMutex
	beacons   map[types.EpochID]types.Hash32

	proposalChansMu sync.Mutex
	proposalChans   map[types.EpochID]chan *proposalMessageWithReceiptData
}

// SetSyncState updates sync state provider. Must be executed only once.
func (tb *TortoiseBeacon) SetSyncState(sync SyncState) {
	if tb.sync != nil {
		tb.Log.Panic("sync state provider can be updated only once")
	}
	tb.sync = sync
}

// Start starts listening for layers and outputs.
func (tb *TortoiseBeacon) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapUint64(&tb.closed, 0, 1) {
		tb.Log.Warning("attempt to start tortoise beacon more than once")
		return nil
	}
	tb.Log.Info("starting %v with the following config: %+v", protoName, tb.config)
	if tb.sync == nil {
		tb.Log.Panic("update sync state provider can't be nil")
	}

	ctx, cancel := context.WithCancel(ctx)
	tb.tg = taskgroup.New(taskgroup.WithContext(ctx))
	tb.cancel = cancel

	tb.initGenesisBeacons()
	tb.layerTicker = tb.clock.Subscribe()

	err := tb.tg.Go(func(ctx context.Context) error {
		tb.listenLayers(ctx)
		return fmt.Errorf("context error: %w", ctx.Err())
	})
	if err != nil {
		tb.Log.Warning("taskgroup: Go returned an error",
			log.Err(err))
	}

	return nil
}

// Close closes TortoiseBeacon.
func (tb *TortoiseBeacon) Close() {
	if !atomic.CompareAndSwapUint64(&tb.closed, 1, 0) {
		return
	}
	tb.Log.Info("closing %v", protoName)
	tb.cancel()
	if err := tb.tg.Wait(); err != nil {
		tb.Log.Warning("taskgroup: Wait returned an error",
			log.Err(err))
	}
	tb.clock.Unsubscribe(tb.layerTicker)
}

// IsClosed returns true if background workers are not running.
func (tb *TortoiseBeacon) IsClosed() bool {
	return atomic.LoadUint64(&tb.closed) == 0
}

// GetBeacon returns a Tortoise Beacon value as []byte for a certain epoch or an error if it doesn't exist.
// TODO(nkryuchkov): consider not using (using DB instead)
func (tb *TortoiseBeacon) GetBeacon(epochID types.EpochID) ([]byte, error) {
	if epochID == 0 {
		return nil, ErrZeroEpoch
	}

	if tb.tortoiseBeaconDB != nil {
		val, err := tb.tortoiseBeaconDB.GetTortoiseBeacon(epochID - 1)
		if err == nil {
			return val.Bytes(), nil
		}

		if !errors.Is(err, database.ErrNotFound) {
			tb.Log.Error("failed to get tortoise beacon for epoch %v from DB: %v", epochID-1, err)

			return nil, fmt.Errorf("get beacon from DB: %w", err)
		}
	}

	if (epochID - 1).IsGenesis() {
		return types.HexToHash32(genesisBeacon).Bytes(), nil
	}

	tb.beaconsMu.RLock()
	defer tb.beaconsMu.RUnlock()

	beacon, ok := tb.beacons[epochID-1]
	if !ok {
		tb.Log.With().Error("beacon is not calculated",
			log.Uint32("target_epoch", uint32(epochID)),
			log.Uint32("beacon_epoch", uint32(epochID-1)))

		return nil, ErrBeaconNotCalculated
	}

	return beacon.Bytes(), nil
}

func (tb *TortoiseBeacon) initGenesisBeacons() {
	closedCh := make(chan struct{})
	close(closedCh)

	for epoch := types.EpochID(0); epoch.IsGenesis(); epoch++ {
		genesis := types.HexToHash32(genesisBeacon)
		tb.beacons[epoch] = genesis

		if tb.tortoiseBeaconDB != nil {
			if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epoch, genesis); err != nil {
				tb.Log.With().Error("failed to write tortoise beacon to DB",
					log.Uint32("epoch_id", uint32(epoch)),
					log.String("beacon", genesis.String()))
			}
		}
	}
}

func (tb *TortoiseBeacon) cleanupVotes() {
	tb.consensusMu.Lock()
	defer tb.consensusMu.Unlock()

	tb.incomingProposals = proposals{}
	tb.firstRoundIncomingVotes = map[string]proposals{}
	tb.votesMargin = map[string]*big.Int{}
	tb.hasVoted = make([]map[string]struct{}, tb.config.RoundsNumber)

	tb.proposalPhaseFinishedTimeMu.Lock()
	defer tb.proposalPhaseFinishedTimeMu.Unlock()

	tb.proposalPhaseFinishedTime = time.Time{}
}

// listens to new layers.
func (tb *TortoiseBeacon) listenLayers(ctx context.Context) {
	tb.Log.With().Info("starting listening layers")

	for {
		select {
		case <-ctx.Done():
			return
		case layer := <-tb.layerTicker:
			tb.Log.With().Info("received tick", layer)
			err := tb.tg.Go(func(ctx context.Context) error {
				tb.handleLayer(ctx, layer)
				return nil
			})
			if err != nil {
				tb.Log.Warning("taskgroup: Go returned an error",
					log.Err(err))
			}
		}
	}
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new CPs.
func (tb *TortoiseBeacon) handleLayer(ctx context.Context, layer types.LayerID) {
	tb.layerMu.Lock()
	if layer.After(tb.lastLayer) {
		tb.Log.With().Debug("updating layer",
			log.Uint32("old_value", tb.lastLayer.Uint32()),
			log.Uint32("new_value", layer.Uint32()))
		tb.lastLayer = layer
	}

	tb.layerMu.Unlock()

	epoch := layer.GetEpoch()

	if !layer.FirstInEpoch() {
		tb.Log.With().Debug("skipping layer because it's not first in this epoch",
			log.Uint32("epoch_id", uint32(epoch)),
			log.Uint32("layer_id", layer.Uint32()))

		return
	}

	tb.Log.With().Info("layer is first in epoch, proceeding",
		log.Uint32("layer", layer.Uint32()))

	tb.seenEpochsMu.Lock()
	if _, ok := tb.seenEpochs[epoch]; ok {
		tb.Log.With().Error("already seen this epoch",
			log.Uint32("epoch_id", uint32(epoch)),
			log.Uint32("layer_id", layer.Uint32()))

		tb.seenEpochsMu.Unlock()

		return
	}

	tb.seenEpochs[epoch] = struct{}{}
	tb.seenEpochsMu.Unlock()

	tb.Log.With().Debug("tortoise beacon got tick, waiting until other nodes have the same epoch",
		log.Uint32("layer", layer.Uint32()),
		log.Uint32("epoch_id", uint32(epoch)),
		log.Duration("wait_time", tb.config.WaitAfterEpochStart))

	epochStartTimer := time.NewTimer(tb.config.WaitAfterEpochStart)
	defer epochStartTimer.Stop()
	select {
	case <-ctx.Done():
	case <-epochStartTimer.C:
		tb.handleEpoch(ctx, epoch)
	}
}

func (tb *TortoiseBeacon) handleEpoch(ctx context.Context, epoch types.EpochID) {
	// TODO(nkryuchkov): check when epoch started, adjust waiting time for this timestamp
	if epoch.IsGenesis() {
		tb.Log.With().Debug("not starting tortoise beacon since we are in genesis epoch",
			log.Uint32("epoch_id", uint32(epoch)))

		return
	}
	if !tb.sync.IsSynced(ctx) {
		tb.Log.With().Info("tortoise beacon protocol is skipped while node is not synced", epoch)
		return
	}

	tb.Log.With().Info("handling epoch",
		log.Uint32("epoch_id", uint32(epoch)))

	defer tb.cleanupVotes()

	tb.proposalChansMu.Lock()
	if epoch > 0 {
		// close channel for previous epoch
		tb.closeProposalChannel(epoch - 1)
	}
	ch := tb.getOrCreateProposalChannel(epoch)
	tb.proposalChansMu.Unlock()

	err := tb.tg.Go(func(ctx context.Context) error {
		tb.readProposalMessagesLoop(ctx, ch)
		return nil
	})
	if err != nil {
		tb.Log.Warning("taskgroup: Go returned an error",
			log.Err(err))
	}

	tb.runProposalPhase(ctx, epoch)
	lastRoundOwnVotes, err := tb.runConsensusPhase(ctx, epoch)
	if err != nil {
		tb.Log.Warning("Consensus execution cancelled",
			log.Err(err))
		return
	}

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	if err := tb.calcBeacon(epoch, lastRoundOwnVotes); err != nil {
		tb.Log.With().Error("failed to calculate beacon",
			log.Uint32("epoch_id", uint32(epoch)),
			log.Err(err))
	}

	tb.Log.With().Debug("finished handling epoch",
		log.Uint32("epoch_id", uint32(epoch)))
}

func (tb *TortoiseBeacon) readProposalMessagesLoop(ctx context.Context, ch chan *proposalMessageWithReceiptData) {
	for {
		select {
		case <-ctx.Done():
			return

		case em := <-ch:
			if em == nil {
				return
			}

			if err := tb.handleProposalMessage(em.message, em.receivedTime); err != nil {
				tb.Log.With().Error("failed to handle proposal message",
					log.String("sender", em.gossip.Sender().String()),
					log.String("message", em.message.String()),
					log.Err(err))

				return
			}

			em.gossip.ReportValidation(ctx, TBProposalProtocol)
		}
	}
}

func (tb *TortoiseBeacon) closeProposalChannel(epoch types.EpochID) {
	if ch, ok := tb.proposalChans[epoch]; ok {
		select {
		case <-ch:
		default:
			close(ch)
			delete(tb.proposalChans, epoch)
		}
	}
}

func (tb *TortoiseBeacon) getOrCreateProposalChannel(epoch types.EpochID) chan *proposalMessageWithReceiptData {
	ch, ok := tb.proposalChans[epoch]
	if !ok {
		ch = make(chan *proposalMessageWithReceiptData, proposalChanCapacity)
		tb.proposalChans[epoch] = ch
	}

	return ch
}

func (tb *TortoiseBeacon) runProposalPhase(ctx context.Context, epoch types.EpochID) {
	tb.Log.With().Debug("starting proposal phase",
		log.Uint32("epoch_id", uint32(epoch)))

	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, tb.config.ProposalDuration)
	defer cancel()

	err := tb.tg.Go(func(ctx context.Context) error {
		tb.Log.With().Debug("starting proposal message sender",
			log.Uint32("epoch_id", uint32(epoch)))

		if err := tb.proposalPhaseImpl(ctx, epoch); err != nil {
			tb.Log.With().Error("failed to send proposal message",
				log.Uint32("epoch_id", uint32(epoch)),
				log.Err(err))
		}

		tb.Log.With().Debug("proposal message sender finished",
			log.Uint32("epoch_id", uint32(epoch)))
		return nil
	})
	if err != nil {
		tb.Log.Warning("taskgroup: Go returned an error",
			log.Err(err))
	}

	<-ctx.Done()
	tb.markProposalPhaseFinished(epoch)

	tb.Log.With().Debug("proposal phase finished",
		log.Uint32("epoch_id", uint32(epoch)))
}

func (tb *TortoiseBeacon) proposalPhaseImpl(ctx context.Context, epoch types.EpochID) error {
	proposedSignature, err := tb.getSignedProposal(epoch)
	if err != nil {
		return fmt.Errorf("calculate signed proposal: %w", err)
	}

	tb.Log.With().Debug("calculated proposal signature",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("signature", string(proposedSignature)))

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return fmt.Errorf("get epoch weight: %w", err)
	}

	passes, err := tb.proposalPassesEligibilityThreshold(proposedSignature, epochWeight)
	if err != nil {
		return fmt.Errorf("proposalPassesEligibilityThreshold: %w", err)
	}

	if !passes {
		tb.Log.With().Debug("proposal to be sent doesn't pass threshold",
			log.Uint32("epoch_id", uint32(epoch)),
			log.String("proposal", string(proposedSignature)),
			log.Uint64("weight", epochWeight))
		// proposal is not sent
		return nil
	}

	tb.Log.With().Debug("Proposal to be sent passes threshold",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("proposal", string(proposedSignature)),
		log.Uint64("weight", epochWeight))

	// concat them into a single proposal message
	m := ProposalMessage{
		EpochID:      epoch,
		NodeID:       tb.nodeID,
		VRFSignature: proposedSignature,
	}

	tb.Log.With().Debug("going to send proposal",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBProposalProtocol, m); err != nil {
		return fmt.Errorf("broadcast proposal message: %w", err)
	}

	tb.Log.With().Info("sent proposal",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("message", m.String()))

	tb.consensusMu.Lock()
	defer tb.consensusMu.Unlock()

	tb.incomingProposals.valid = append(tb.incomingProposals.valid, proposedSignature)

	return nil
}

// runConsensusPhase runs K voting rounds and returns result from last weak coin round.
func (tb *TortoiseBeacon) runConsensusPhase(ctx context.Context, epoch types.EpochID) (allVotes, error) {
	tb.Log.With().Debug("starting consensus phase",
		log.Uint32("epoch_id", uint32(epoch)))

	tb.startWeakCoinEpoch(epoch)
	defer tb.fininshWeakCoinEpoch()

	// For K rounds: In each round that lasts δ, wait for proposals to come in.
	// For next rounds,
	// wait for δ time, and construct a message that points to all messages from previous round received by δ.
	// rounds 1 to K
	timer := time.NewTimer(tb.config.FirstVotingRoundDuration + tb.config.WeakCoinRoundDuration)
	defer timer.Stop()

	var (
		coinFlip            bool
		ownLastRoundVotesMu sync.RWMutex
		ownLastRoundVotes   allVotes
	)

	for round := firstRound; round <= tb.lastRound(); round++ {
		// always use coinflip from the previous round for current round.
		// round 1 is running without coinflip (e.g. value is false) intentionally
		round := round
		previousCoinFlip := coinFlip
		err := tb.tg.Go(func(ctx context.Context) error {
			if round == firstRound {
				if err := tb.sendProposalVote(ctx, epoch); err != nil {
					tb.Log.With().Error("Failed to send proposal vote",
						log.Uint32("epoch_id", uint32(epoch)),
						log.Uint32("round_id", uint32(round)),
						log.Err(err))

					return fmt.Errorf("failed to send proposal vote: %w", err)
				}

				return nil
			}

			// next rounds, send vote
			// construct a message that points to all messages from previous round received by δ
			ownCurrentRoundVotes, err := tb.calcVotes(epoch, round, previousCoinFlip)
			if err != nil {
				tb.Log.With().Error("Failed to calculate votes",
					log.Uint32("epoch_id", uint32(epoch)),
					log.Uint32("round_id", uint32(round)),
					log.Err(err))

				return fmt.Errorf("calculate votes: %w", err)
			}

			if round == tb.lastRound() {
				ownLastRoundVotesMu.Lock()
				ownLastRoundVotes = ownCurrentRoundVotes
				ownLastRoundVotesMu.Unlock()
			}

			if err := tb.sendFollowingVote(ctx, epoch, round, ownCurrentRoundVotes); err != nil {
				tb.Log.With().Error("Failed to send following vote",
					log.Uint32("epoch_id", uint32(epoch)),
					log.Uint32("round_id", uint32(round)),
					log.Err(err))

				return fmt.Errorf("send following vote: %w", err)
			}

			return nil
		})
		if err != nil {
			tb.Log.Warning("taskgroup: Go returned an error",
				log.Err(err))
		}

		err = tb.tg.Go(func(ctx context.Context) error {
			tb.startWeakCoinRound(ctx, epoch, round)
			return nil
		})
		if err != nil {
			tb.Log.Warning("taskgroup: Go returned an error",
				log.Err(err))
		}

		select {
		case <-timer.C:
			timer.Reset(tb.config.VotingRoundDuration + tb.config.WeakCoinRoundDuration)
		case <-ctx.Done():
			return allVotes{}, ctx.Err()
		}

		tb.weakCoin.FinishRound()

		coinFlip = tb.weakCoin.Get(epoch, round)
	}

	tb.Log.With().Debug("Consensus phase finished",
		log.Uint32("epoch_id", uint32(epoch)))

	ownLastRoundVotesMu.RLock()
	defer ownLastRoundVotesMu.RUnlock()

	return ownLastRoundVotes, nil
}

func (tb *TortoiseBeacon) startWeakCoinEpoch(epoch types.EpochID) {
	// we need to pass a map with spacetime unit allowances before any round is started
	_, atxs, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		tb.Log.With().Panic("unable to load list of atxs", log.Err(err))
	}

	ua := weakcoin.UnitAllowances{}
	for _, id := range atxs {
		header, err := tb.atxDB.GetAtxHeader(id)
		if err != nil {
			tb.Log.With().Panic("unable to load atx header", log.Err(err))
		}
		ua[string(header.NodeID.VRFPublicKey)] += uint64(header.NumUnits)
	}

	tb.weakCoin.StartEpoch(epoch, ua)
}

func (tb *TortoiseBeacon) fininshWeakCoinEpoch() {
	tb.weakCoin.FinishEpoch()
}

func (tb *TortoiseBeacon) markProposalPhaseFinished(epoch types.EpochID) {
	finishedAt := time.Now()

	tb.proposalPhaseFinishedTimeMu.Lock()
	tb.proposalPhaseFinishedTime = finishedAt
	tb.proposalPhaseFinishedTimeMu.Unlock()

	tb.Debug("marked proposal phase for epoch %v finished at %v", epoch, finishedAt.String())
}

func (tb *TortoiseBeacon) receivedBeforeProposalPhaseFinished(epoch types.EpochID, receivedAt time.Time) bool {
	tb.proposalPhaseFinishedTimeMu.RLock()
	finishedAt := tb.proposalPhaseFinishedTime
	tb.proposalPhaseFinishedTimeMu.RUnlock()
	hasFinished := !finishedAt.IsZero()

	tb.Debug("checking if timestamp %v was received before proposal phase finished in epoch %v, is phase finished: %v, finished at: %v", receivedAt.String(), epoch, hasFinished, finishedAt.String())

	return !hasFinished || receivedAt.Before(finishedAt)
}

func (tb *TortoiseBeacon) startWeakCoinRound(ctx context.Context, epoch types.EpochID, round types.RoundID) {
	timeout := tb.config.FirstVotingRoundDuration
	if round > firstRound {
		timeout = tb.config.VotingRoundDuration
	}
	t := time.NewTimer(timeout)
	defer t.Stop()

	select {
	case <-t.C:
		break
	case <-ctx.Done():
		return
	}

	// TODO(nkryuchkov):
	// should be published only after we should have received them
	if err := tb.weakCoin.StartRound(ctx, round); err != nil {
		tb.Log.With().Error("failed to publish weak coin proposal",
			log.Uint32("epoch_id", uint32(epoch)),
			log.Uint32("round_id", uint32(round)),
			log.Err(err))
	}
}

func (tb *TortoiseBeacon) sendProposalVote(ctx context.Context, epoch types.EpochID) error {
	// round 1, send hashed proposal
	// create a voting message that references all seen proposals within δ time frame and send it

	// TODO(nkryuchkov): also send a bit vector
	// TODO(nkryuchkov): initialize margin vector to initial votes
	// TODO(nkryuchkov): use weight
	return tb.sendFirstRoundVote(ctx, epoch, tb.incomingProposals)
}

func (tb *TortoiseBeacon) sendFirstRoundVote(ctx context.Context, epoch types.EpochID, proposals proposals) error {
	mb := FirstVotingMessageBody{
		ValidProposals:            proposals.valid,
		PotentiallyValidProposals: proposals.potentiallyValid,
	}

	sig, err := tb.signMessage(mb)
	if err != nil {
		return fmt.Errorf("signMessage: %w", err)
	}

	m := FirstVotingMessage{
		FirstVotingMessageBody: mb,
		Signature:              sig,
	}

	tb.Log.With().Debug("sending first round vote",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(firstRound)),
		log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBFirstVotingProtocol, m); err != nil {
		return fmt.Errorf("sendToGossip: %w", err)
	}

	return nil
}

func (tb *TortoiseBeacon) sendFollowingVote(ctx context.Context, epoch types.EpochID, round types.RoundID, ownCurrentRoundVotes allVotes) error {
	tb.consensusMu.RLock()
	bitVector := tb.encodeVotes(ownCurrentRoundVotes, tb.incomingProposals)
	tb.consensusMu.RUnlock()

	mb := FollowingVotingMessageBody{
		RoundID:        round,
		VotesBitVector: bitVector,
	}

	sig, err := tb.signMessage(mb)
	if err != nil {
		return fmt.Errorf("getSignedProposal: %w", err)
	}

	m := FollowingVotingMessage{
		FollowingVotingMessageBody: mb,
		Signature:                  sig,
	}

	tb.Log.With().Debug("sending following round vote",
		log.Uint32("epoch_id", uint32(epoch)),
		log.Uint32("round_id", uint32(round)),
		log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBFollowingVotingProtocol, m); err != nil {
		return fmt.Errorf("broadcast voting message: %w", err)
	}

	return nil
}

func (tb *TortoiseBeacon) votingThreshold(epochWeight uint64) *big.Int {
	v, _ := new(big.Float).Mul(
		new(big.Float).SetRat(tb.config.Theta),
		new(big.Float).SetUint64(epochWeight),
	).Int(nil)

	return v
}

// TODO(nkryuchkov): Consider replacing github.com/ALTree/bigfloat.
func (tb *TortoiseBeacon) atxThresholdFraction(epochWeight uint64) (*big.Float, error) {
	if epochWeight == 0 {
		return big.NewFloat(0), ErrZeroEpochWeight
	}

	// threshold(k, q, W) = 1 - (2 ^ (- (k/((1-q)*W))
	// Floating point: 1 - math.Pow(2.0, -(float64(tb.config.Kappa)/((1.0-tb.config.Q)*float64(epochWeight))))
	// Fixed point:
	v := new(big.Float).Sub(
		new(big.Float).SetInt64(1),
		bigfloat.Pow(
			new(big.Float).SetInt64(2),
			new(big.Float).SetRat(
				new(big.Rat).Neg(
					new(big.Rat).Quo(
						new(big.Rat).SetUint64(tb.config.Kappa),
						new(big.Rat).Mul(
							new(big.Rat).Sub(
								new(big.Rat).SetInt64(1.0),
								tb.config.Q,
							),
							new(big.Rat).SetUint64(epochWeight),
						),
					),
				),
			),
		),
	)

	return v, nil
}

// TODO(nkryuchkov): Consider having a generic function for probabilities.
func (tb *TortoiseBeacon) atxThreshold(epochWeight uint64) (*big.Int, error) {
	const signatureLength = 64 * 8

	fraction, err := tb.atxThresholdFraction(epochWeight)
	if err != nil {
		return nil, err
	}

	two := big.NewInt(2)
	signatureLengthBigInt := big.NewInt(signatureLength)

	maxPossibleNumberBigInt := new(big.Int).Exp(two, signatureLengthBigInt, nil)
	maxPossibleNumberBigFloat := new(big.Float).SetInt(maxPossibleNumberBigInt)

	thresholdBigFloat := new(big.Float).Mul(maxPossibleNumberBigFloat, fraction)
	threshold, _ := thresholdBigFloat.Int(nil)

	return threshold, nil
}

func (tb *TortoiseBeacon) getSignedProposal(epoch types.EpochID) ([]byte, error) {
	p, err := tb.buildProposal(epoch)
	if err != nil {
		return nil, fmt.Errorf("calculate proposal: %w", err)
	}

	signature := tb.vrfSigner.Sign(p)
	tb.Log.With().Debug("calculated signature",
		log.Uint32("epoch_id", uint32(epoch)),
		log.String("proposal", util.Bytes2Hex(p)),
		log.String("signature", string(signature)))

	return signature, nil
}

func (tb *TortoiseBeacon) signMessage(message interface{}) ([]byte, error) {
	encoded, err := types.InterfaceToBytes(message)
	if err != nil {
		return nil, fmt.Errorf("InterfaceToBytes: %w", err)
	}

	return tb.edSigner.Sign(encoded), nil
}

func (tb *TortoiseBeacon) buildProposal(epoch types.EpochID) ([]byte, error) {
	message := &struct {
		Prefix string
		Epoch  uint32
	}{
		Prefix: proposalPrefix,
		Epoch:  uint32(epoch),
	}

	b, err := types.InterfaceToBytes(message)
	if err != nil {
		return nil, fmt.Errorf("InterfaceToBytes: %w", err)
	}

	return b, nil
}

func ceilDuration(duration, multiple time.Duration) time.Duration {
	result := duration.Truncate(multiple)
	if duration%multiple != 0 {
		result += multiple
	}

	return result
}

func (tb *TortoiseBeacon) sendToGossip(ctx context.Context, channel string, data interface{}) error {
	serialized, err := types.InterfaceToBytes(data)
	if err != nil {
		return fmt.Errorf("serializing: %w", err)
	}

	if err := tb.net.Broadcast(ctx, channel, serialized); err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}

	return nil
}

func (tb *TortoiseBeacon) proposalPassesEligibilityThreshold(proposal []byte, epochWeight uint64) (bool, error) {
	proposalInt := new(big.Int).SetBytes(proposal)

	threshold, err := tb.atxThreshold(epochWeight)
	if err != nil {
		return false, fmt.Errorf("atxThreshold: %w", err)
	}

	tb.Log.With().Debug("checking proposal for atx threshold",
		log.String("proposal", proposalInt.String()),
		log.String("threshold", threshold.String()))

	return proposalInt.Cmp(threshold) == -1, nil
}
