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
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
	"golang.org/x/sync/errgroup"
)

const (
	protoName            = "TORTOISE_BEACON_PROTOCOL"
	proposalPrefix       = "TBP"
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

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./tortoise_beacon.go

type coin interface {
	StartEpoch(context.Context, types.EpochID, weakcoin.UnitAllowances)
	StartRound(context.Context, types.RoundID) error
	FinishRound(context.Context)
	Get(context.Context, types.EpochID, types.RoundID) bool
	FinishEpoch(context.Context, types.EpochID)
	HandleSerializedMessage(context.Context, service.GossipMessage, service.Fetcher)
}

type eligibilityChecker interface {
	IsProposalEligible(proposal []byte) bool
}

type (
	proposals = struct{ valid, potentiallyValid [][]byte }
	allVotes  = struct{ valid, invalid proposalSet }
)

type layerClock interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timesync.LayerTimer)
	LayerToTime(types.LayerID) time.Time
}

// SyncState interface to check the state the sync.
type SyncState interface {
	IsSynced(context.Context) bool
}

// New returns a new TortoiseBeacon.
func New(
	conf Config,
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
		logger:                  logger,
		config:                  conf,
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
		proposalChans:           make(map[types.EpochID]chan *proposalMessageWithReceiptData),
		votesMargin:             map[string]*big.Int{},
	}
}

// TortoiseBeacon represents Tortoise Beacon.
type TortoiseBeacon struct {
	closed uint64
	eg     errgroup.Group
	cancel context.CancelFunc

	logger log.Log

	config           Config
	nodeID           types.NodeID
	sync             SyncState
	net              broadcaster
	atxDB            activationDB
	tortoiseBeaconDB tortoiseBeaconDB
	edSigner         signing.Signer
	edVerifier       signing.VerifyExtractor
	vrfSigner        signing.Signer
	vrfVerifier      signing.Verifier
	weakCoin         coin

	clock       layerClock
	layerTicker chan types.LayerID

	mu              sync.RWMutex
	epochInProgress types.EpochID
	epochWeight     uint64
	// TODO(nkryuchkov): have a mixed list of all sorted proposals
	// have one bit vector: valid proposals
	incomingProposals       proposals
	firstRoundIncomingVotes map[string]proposals // sorted votes for bit vector decoding
	// TODO(nkryuchkov): For every round excluding first round consider having a vector of opinions.
	votesMargin               map[string]*big.Int
	hasVoted                  []map[string]struct{}
	proposalPhaseFinishedTime time.Time
	beacons                   map[types.EpochID]types.Hash32
	proposalChans             map[types.EpochID]chan *proposalMessageWithReceiptData
	proposalChecker           eligibilityChecker
}

// SetSyncState updates sync state provider. Must be executed only once.
func (tb *TortoiseBeacon) SetSyncState(sync SyncState) {
	if tb.sync != nil {
		tb.logger.Panic("sync state provider can be updated only once")
	}
	tb.sync = sync
}

// Start starts listening for layers and outputs.
func (tb *TortoiseBeacon) Start(ctx context.Context) error {
	if !atomic.CompareAndSwapUint64(&tb.closed, 0, 1) {
		tb.logger.WithContext(ctx).Warning("attempt to start tortoise beacon more than once")
		return nil
	}
	tb.logger.Info("starting %v with the following config: %+v", protoName, tb.config)
	if tb.sync == nil {
		tb.logger.Panic("update sync state provider can't be nil")
	}

	ctx, cancel := context.WithCancel(ctx)
	tb.cancel = cancel

	tb.initGenesisBeacons()
	tb.layerTicker = tb.clock.Subscribe()

	tb.eg.Go(func() error {
		tb.listenLayers(ctx)
		return fmt.Errorf("context error: %w", ctx.Err())
	})

	return nil
}

// Close closes TortoiseBeacon.
func (tb *TortoiseBeacon) Close() {
	if !atomic.CompareAndSwapUint64(&tb.closed, 1, 0) {
		return
	}
	tb.logger.Info("closing %v", protoName)
	tb.cancel()
	tb.logger.Info("waiting for tortoise beacon goroutines to finish")
	if err := tb.eg.Wait(); err != nil {
		tb.logger.With().Info("received error waiting for goroutines to finish", log.Err(err))
	}
	tb.logger.Info("tortoise beacon goroutines finished")
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
			tb.logger.Error("failed to get tortoise beacon for epoch %v from DB: %v", epochID-1, err)

			return nil, fmt.Errorf("get beacon from DB: %w", err)
		}
	}

	if (epochID - 1).IsGenesis() {
		return types.HexToHash32(genesisBeacon).Bytes(), nil
	}

	tb.mu.RLock()
	defer tb.mu.RUnlock()

	beacon, ok := tb.beacons[epochID-1]
	if !ok {
		tb.logger.With().Error("beacon is not calculated",
			log.Uint32("target_epoch", uint32(epochID)),
			log.Uint32("beacon_epoch", uint32(epochID-1)))

		return nil, ErrBeaconNotCalculated
	}

	return beacon.Bytes(), nil
}

func (tb *TortoiseBeacon) setBeacon(epoch types.EpochID, beacon types.Hash32) {
	tb.mu.Lock()
	tb.beacons[epoch] = beacon
	tb.mu.Unlock()
}

func (tb *TortoiseBeacon) initGenesisBeacons() {
	for epoch := types.EpochID(0); epoch.IsGenesis(); epoch++ {
		genesis := types.HexToHash32(genesisBeacon)
		tb.beacons[epoch] = genesis

		if tb.tortoiseBeaconDB != nil {
			if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epoch, genesis); err != nil {
				tb.logger.With().Error("failed to write tortoise beacon to DB",
					log.Uint32("epoch_id", uint32(epoch)),
					log.String("beacon", genesis.String()))
			}
		}
	}
}

func (tb *TortoiseBeacon) cleanupVotes() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.incomingProposals = proposals{}
	tb.firstRoundIncomingVotes = map[string]proposals{}
	tb.votesMargin = map[string]*big.Int{}
	tb.hasVoted = make([]map[string]struct{}, tb.config.RoundsNumber)
	tb.proposalPhaseFinishedTime = time.Time{}
}

// listens to new layers.
func (tb *TortoiseBeacon) listenLayers(ctx context.Context) {
	tb.logger.With().Info("starting listening layers")

	for {
		select {
		case <-ctx.Done():
			return
		case layer := <-tb.layerTicker:
			tb.logger.With().Debug("received tick", layer)
			tb.eg.Go(func() error {
				tb.handleLayer(ctx, layer)
				return nil
			})
		}
	}
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new CPs.
func (tb *TortoiseBeacon) handleLayer(ctx context.Context, layer types.LayerID) {
	epoch := layer.GetEpoch()
	logger := tb.logger.WithContext(ctx).WithFields(layer, epoch)

	if !layer.FirstInEpoch() {
		logger.Debug("not first layer in epoch, skipping")
		return
	}
	logger.Info("first layer in epoch, proceeding")

	tb.mu.Lock()
	if tb.epochInProgress >= epoch {
		tb.mu.Unlock()
		logger.Panic("epoch ticked twice")
	}
	tb.epochInProgress = epoch
	tb.mu.Unlock()

	logger.With().Debug("tortoise beacon got tick, waiting until other nodes have the same epoch",
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
	ctx = log.WithNewSessionID(ctx)
	logger := tb.logger.WithContext(ctx).WithFields(epoch)
	// TODO(nkryuchkov): check when epoch started, adjust waiting time for this timestamp
	if epoch.IsGenesis() {
		logger.Debug("not starting tortoise beacon since we are in genesis epoch")
		return
	}
	if !tb.sync.IsSynced(ctx) {
		logger.Info("tortoise beacon protocol is skipped while node is not synced")
		return
	}

	logger.Info("handling epoch")

	defer tb.cleanupVotes()

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		logger.With().Error("failed to get weight targeting epoch", log.Err(err))
		return
	}
	if epochWeight == 0 {
		logger.With().Error("zero weight targeting epoch", log.Err(ErrZeroEpochWeight))
	}

	tb.mu.Lock()
	tb.epochWeight = epochWeight
	tb.proposalChecker = createProposalChecker(tb.config.Kappa, tb.config.Q, epochWeight, logger)
	if epoch > 0 {
		// close channel for previous epoch
		tb.closeProposalChannel(epoch - 1)
	}
	ch := tb.getOrCreateProposalChannel(epoch)
	tb.mu.Unlock()

	tb.eg.Go(func() error {
		tb.readProposalMessagesLoop(ctx, ch)
		return nil
	})

	tb.runProposalPhase(ctx, epoch)
	lastRoundOwnVotes, err := tb.runConsensusPhase(ctx, epoch)
	if err != nil {
		logger.With().Warning("Consensus execution cancelled", log.Err(err))
		return
	}

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	if err := tb.calcBeacon(ctx, epoch, lastRoundOwnVotes); err != nil {
		logger.With().Error("failed to calculate beacon", log.Err(err))
	}

	logger.With().Debug("finished handling epoch")
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
	logger := tb.logger.WithContext(ctx).WithFields(epoch)
	logger.Debug("starting proposal phase")

	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, tb.config.ProposalDuration)
	defer cancel()

	tb.eg.Go(func() error {
		logger.Debug("starting proposal message sender")

		if err := tb.proposalPhaseImpl(ctx, epoch); err != nil {
			logger.With().Error("failed to send proposal message", log.Err(err))
		}

		logger.Debug("proposal message sender finished")
		return nil
	})

	<-ctx.Done()
	tb.markProposalPhaseFinished(epoch)

	logger.Debug("proposal phase finished")
}

func (tb *TortoiseBeacon) proposalPhaseImpl(ctx context.Context, epoch types.EpochID) error {
	logger := tb.logger.WithContext(ctx).WithFields(epoch)
	proposedSignature := buildSignedProposal(ctx, tb.vrfSigner, epoch, tb.logger)

	logger.With().Debug("calculated proposal signature",
		log.String("signature", string(proposedSignature)))

	passes := tb.proposalChecker.IsProposalEligible(proposedSignature)
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
		NodeID:       tb.nodeID,
		VRFSignature: proposedSignature,
	}

	logger.With().Debug("going to send proposal", log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBProposalProtocol, m); err != nil {
		return fmt.Errorf("broadcast proposal message: %w", err)
	}

	logger.With().Info("sent proposal", log.String("message", m.String()))

	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.incomingProposals.valid = append(tb.incomingProposals.valid, proposedSignature)

	return nil
}

// runConsensusPhase runs K voting rounds and returns result from last weak coin round.
func (tb *TortoiseBeacon) runConsensusPhase(ctx context.Context, epoch types.EpochID) (allVotes, error) {
	logger := tb.logger.WithContext(ctx).WithFields(epoch)
	logger.Debug("starting consensus phase")

	tb.startWeakCoinEpoch(ctx, epoch)
	defer tb.fininshWeakCoinEpoch(ctx, epoch)

	// For K rounds: In each round that lasts δ, wait for votes to come in.
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

	for round := types.FirstRound; round <= tb.lastRound(); round++ {
		// always use coinflip from the previous round for current round.
		// round 1 is running without coinflip (e.g. value is false) intentionally
		round := round
		rLogger := logger.WithFields(round)
		previousCoinFlip := coinFlip
		tb.eg.Go(func() error {
			if round == types.FirstRound {
				if err := tb.sendProposalVote(ctx, epoch); err != nil {
					rLogger.With().Error("Failed to send proposal vote", log.Err(err))
					return fmt.Errorf("failed to send proposal vote: %w", err)
				}
				return nil
			}

			// next rounds, send vote
			// construct a message that points to all messages from previous round received by δ
			ownCurrentRoundVotes, err := tb.calcVotes(ctx, epoch, round, previousCoinFlip)
			if err != nil {
				rLogger.With().Error("failed to calculate votes", log.Err(err))
				return fmt.Errorf("calculate votes: %w", err)
			}

			if round == tb.lastRound() {
				ownLastRoundVotesMu.Lock()
				ownLastRoundVotes = ownCurrentRoundVotes
				ownLastRoundVotesMu.Unlock()
			}

			if err := tb.sendFollowingVote(ctx, epoch, round, ownCurrentRoundVotes); err != nil {
				rLogger.With().Error("Failed to send following vote", log.Err(err))
				return fmt.Errorf("send following vote: %w", err)
			}
			return nil
		})

		tb.eg.Go(func() error {
			tb.startWeakCoinRound(ctx, epoch, round)
			return nil
		})

		select {
		case <-timer.C:
			timer.Reset(tb.config.VotingRoundDuration + tb.config.WeakCoinRoundDuration)
		case <-ctx.Done():
			return allVotes{}, ctx.Err()
		}

		tb.weakCoin.FinishRound(ctx)

		coinFlip = tb.weakCoin.Get(ctx, epoch, round)
	}

	logger.Debug("Consensus phase finished")

	ownLastRoundVotesMu.RLock()
	defer ownLastRoundVotesMu.RUnlock()

	return ownLastRoundVotes, nil
}

func (tb *TortoiseBeacon) startWeakCoinEpoch(ctx context.Context, epoch types.EpochID) {
	// we need to pass a map with spacetime unit allowances before any round is started
	_, atxs, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		tb.logger.WithContext(ctx).With().Panic("unable to load list of atxs", log.Err(err))
	}

	ua := weakcoin.UnitAllowances{}
	for _, id := range atxs {
		header, err := tb.atxDB.GetAtxHeader(id)
		if err != nil {
			tb.logger.WithContext(ctx).With().Panic("unable to load atx header", log.Err(err))
		}
		ua[string(header.NodeID.VRFPublicKey)] += uint64(header.NumUnits)
	}

	tb.weakCoin.StartEpoch(ctx, epoch, ua)
}

func (tb *TortoiseBeacon) fininshWeakCoinEpoch(ctx context.Context, epoch types.EpochID) {
	tb.weakCoin.FinishEpoch(ctx, epoch)
}

func (tb *TortoiseBeacon) markProposalPhaseFinished(epoch types.EpochID) {
	finishedAt := time.Now()
	tb.mu.Lock()
	tb.proposalPhaseFinishedTime = finishedAt
	tb.mu.Unlock()
	tb.logger.Debug("marked proposal phase for epoch %v finished at %v", epoch, finishedAt.String())
}

func (tb *TortoiseBeacon) receivedBeforeProposalPhaseFinished(epoch types.EpochID, receivedAt time.Time) bool {
	tb.mu.RLock()
	finishedAt := tb.proposalPhaseFinishedTime
	tb.mu.RUnlock()
	hasFinished := !finishedAt.IsZero()

	tb.logger.Debug("checking if timestamp %v was received before proposal phase finished in epoch %v, is phase finished: %v, finished at: %v", receivedAt.String(), epoch, hasFinished, finishedAt.String())

	return !hasFinished || receivedAt.Before(finishedAt)
}

func (tb *TortoiseBeacon) startWeakCoinRound(ctx context.Context, epoch types.EpochID, round types.RoundID) {
	timeout := tb.config.FirstVotingRoundDuration
	if round > 0 {
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
		tb.logger.WithContext(ctx).With().Error("failed to publish weak coin proposal",
			epoch,
			round,
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

	sig := signMessage(tb.edSigner, mb, tb.logger)

	m := FirstVotingMessage{
		FirstVotingMessageBody: mb,
		Signature:              sig,
	}

	tb.logger.WithContext(ctx).With().Debug("sending first round vote",
		epoch,
		types.FirstRound,
		log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBFirstVotingProtocol, m); err != nil {
		return fmt.Errorf("sendToGossip: %w", err)
	}

	return nil
}

func (tb *TortoiseBeacon) sendFollowingVote(ctx context.Context, epoch types.EpochID, round types.RoundID, ownCurrentRoundVotes allVotes) error {
	tb.mu.RLock()
	bitVector := tb.encodeVotes(ownCurrentRoundVotes, tb.incomingProposals)
	tb.mu.RUnlock()

	mb := FollowingVotingMessageBody{
		RoundID:        round,
		VotesBitVector: bitVector,
	}

	sig := signMessage(tb.edSigner, mb, tb.logger)

	m := FollowingVotingMessage{
		FollowingVotingMessageBody: mb,
		Signature:                  sig,
	}

	tb.logger.WithContext(ctx).With().Debug("sending following round vote",
		epoch,
		round,
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

type proposalChecker struct {
	threshold *big.Int
}

func createProposalChecker(kappa uint64, q *big.Rat, epochWeight uint64, logger log.Log) *proposalChecker {
	if epochWeight == 0 {
		logger.Panic("creating proposal checker with zero weight")
	}

	threshold := atxThreshold(kappa, q, epochWeight)
	logger.With().Debug("created proposal checker with ATX threshold",
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

func (tb *TortoiseBeacon) sendToGossip(ctx context.Context, channel string, data interface{}) error {
	serialized, err := types.InterfaceToBytes(data)
	if err != nil {
		tb.logger.With().Panic("failed to serialize message for gossip", log.Err(err))
	}

	if err := tb.net.Broadcast(ctx, channel, serialized); err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}

	return nil
}
