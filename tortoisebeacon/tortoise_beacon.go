package tortoisebeacon

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ALTree/bigfloat"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
)

const (
	protoName            = "TORTOISE_BEACON_PROTOCOL"
	proposalPrefix       = "TBP"
	cleanupInterval      = 30 * time.Second
	cleanupEpochs        = 1000
	firstRound           = types.RoundID(1)
	genesisBeacon        = "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	proposalChanCapacity = 1024
)

// TODO(nkryuchkov): remove unused errors
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

type epochRoundPair struct {
	EpochID types.EpochID
	Round   types.RoundID
}

type (
	nodeID          = string
	proposal        = string
	hashSet         = map[proposal]struct{}
	firstRoundVotes = struct {
		ValidVotes            proposalList
		PotentiallyValidVotes proposalList
	}
	firstRoundVotesPerPK    = map[nodeID]firstRoundVotes
	votesPerPK              = map[nodeID]votesSetPair
	firstRoundVotesPerEpoch = map[types.EpochID]firstRoundVotesPerPK
	votesPerRound           = map[epochRoundPair]votesPerPK
	ownVotes                = map[epochRoundPair]votesSetPair
	votesMarginMap          = map[proposal]int
	proposalsMap            = map[types.EpochID]hashSet
)

// TortoiseBeacon represents Tortoise Beacon.
type TortoiseBeacon struct {
	util.Closer
	log.Log

	config        Config
	minerID       types.NodeID
	layerDuration time.Duration

	net              broadcaster
	atxDB            activationDB
	tortoiseBeaconDB tortoiseBeaconDB
	edSigner         *signing.EdSigner
	vrfVerifier      verifierFunc
	vrfSigner        signer
	weakCoin         weakcoin.WeakCoin

	seenEpochsMu sync.Mutex
	seenEpochs   map[types.EpochID]struct{}

	clock                    layerClock
	layerTicker              chan types.LayerID
	q                        *big.Rat
	gracePeriodDuration      time.Duration
	proposalDuration         time.Duration
	firstVotingRoundDuration time.Duration
	votingRoundDuration      time.Duration
	weakCoinRoundDuration    time.Duration
	waitAfterEpochStart      time.Duration

	currentRoundsMu sync.RWMutex
	currentRounds   map[types.EpochID]types.RoundID

	validProposalsMu sync.RWMutex
	validProposals   proposalsMap

	potentiallyValidProposalsMu sync.RWMutex
	potentiallyValidProposals   proposalsMap

	votesMu                           sync.RWMutex
	firstRoundIncomingVotes           firstRoundVotesPerEpoch           // all rounds - votes (decoded votes)
	firstRoundOutcomingVotes          map[types.EpochID]firstRoundVotes // all rounds - votes (decoded votes)
	incomingVotes                     votesPerRound                     // all rounds - votes (decoded votes)
	ownVotes                          ownVotes                          // all rounds - own votes
	proposalPhaseFinishedTimestampsMu sync.RWMutex
	proposalPhaseFinishedTimestamps   map[types.EpochID]time.Time

	beaconsMu sync.RWMutex
	beacons   map[types.EpochID]types.Hash32

	proposalChansMu sync.Mutex
	proposalChans   map[types.EpochID]chan *proposalMessageWithReceiptData

	backgroundWG sync.WaitGroup

	layerMu   sync.RWMutex
	lastLayer types.LayerID
}

// a function to verify the message with the signature and its public key.
type verifierFunc = func(pub, msg, sig []byte) bool

type signer interface {
	Sign(msg []byte) []byte
}

type layerClock interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timer timesync.LayerTimer)
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
	LayerToTime(id types.LayerID) time.Time
}

// New returns a new TortoiseBeacon.
func New(
	conf Config,
	minerID types.NodeID,
	layerDuration time.Duration,
	net broadcaster,
	atxDB activationDB,
	tortoiseBeaconDB tortoiseBeaconDB,
	edSigner *signing.EdSigner,
	vrfVerifier verifierFunc,
	vrfSigner signer,
	weakCoin weakcoin.WeakCoin,
	clock layerClock,
	logger log.Log,
) *TortoiseBeacon {
	q, ok := new(big.Rat).SetString(conf.Q)
	if !ok {
		panic("bad q parameter")
	}

	return &TortoiseBeacon{
		Log:                             logger,
		Closer:                          util.NewCloser(),
		config:                          conf,
		minerID:                         minerID,
		layerDuration:                   layerDuration,
		net:                             net,
		atxDB:                           atxDB,
		tortoiseBeaconDB:                tortoiseBeaconDB,
		edSigner:                        edSigner,
		vrfVerifier:                     vrfVerifier,
		vrfSigner:                       vrfSigner,
		weakCoin:                        weakCoin,
		clock:                           clock,
		q:                               q,
		gracePeriodDuration:             time.Duration(conf.GracePeriodDurationMs) * time.Millisecond,
		proposalDuration:                time.Duration(conf.ProposalDurationMs) * time.Millisecond,
		firstVotingRoundDuration:        time.Duration(conf.FirstVotingRoundDurationMs) * time.Millisecond,
		votingRoundDuration:             time.Duration(conf.VotingRoundDurationMs) * time.Millisecond,
		weakCoinRoundDuration:           time.Duration(conf.WeakCoinRoundDurationMs) * time.Millisecond,
		waitAfterEpochStart:             time.Duration(conf.WaitAfterEpochStart) * time.Millisecond,
		currentRounds:                   make(map[types.EpochID]types.RoundID),
		validProposals:                  make(map[types.EpochID]hashSet),
		potentiallyValidProposals:       make(map[types.EpochID]hashSet),
		ownVotes:                        make(ownVotes),
		beacons:                         make(map[types.EpochID]types.Hash32),
		proposalPhaseFinishedTimestamps: make(map[types.EpochID]time.Time),
		incomingVotes:                   make(map[epochRoundPair]votesPerPK),
		firstRoundIncomingVotes:         make(map[types.EpochID]firstRoundVotesPerPK),
		firstRoundOutcomingVotes:        make(map[types.EpochID]firstRoundVotes),
		seenEpochs:                      make(map[types.EpochID]struct{}),
		proposalChans:                   make(map[types.EpochID]chan *proposalMessageWithReceiptData),
	}
}

// Start starts listening for layers and outputs.
func (tb *TortoiseBeacon) Start(ctx context.Context) error {
	tb.Log.Info("Starting %v with the following config: %+v", protoName, tb.config)

	tb.initGenesisBeacons()

	tb.layerTicker = tb.clock.Subscribe()

	tb.backgroundWG.Add(1)

	go func() {
		defer tb.backgroundWG.Done()

		tb.listenLayers(ctx)
	}()

	tb.backgroundWG.Add(1)

	go func() {
		defer tb.backgroundWG.Done()

		tb.cleanupLoop()
	}()

	return nil
}

// Close closes TortoiseBeacon.
func (tb *TortoiseBeacon) Close() {
	tb.Log.Info("Closing %v", protoName)
	tb.Closer.Close()
	tb.backgroundWG.Wait() // Wait until background goroutines finish
	tb.clock.Unsubscribe(tb.layerTicker)
}

// GetBeacon returns a Tortoise Beacon value as []byte for a certain epoch or an error if it doesn't exist.
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
			tb.Log.Error("Failed to get tortoise beacon for epoch %v from DB: %v", epochID-1, err)

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
		tb.Log.With().Error("Beacon is not calculated",
			log.Uint64("target_epoch", uint64(epochID)),
			log.Uint64("beacon_epoch", uint64(epochID-1)))

		return nil, ErrBeaconNotCalculated
	}

	return beacon.Bytes(), nil
}

func (tb *TortoiseBeacon) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tb.CloseChannel():
			return
		case <-ticker.C:
			tb.cleanup()
		}
	}
}

func (tb *TortoiseBeacon) initGenesisBeacons() {
	closedCh := make(chan struct{})
	close(closedCh)

	epoch := types.EpochID(0)
	for ; epoch.IsGenesis(); epoch++ {
		genesis := types.HexToHash32(genesisBeacon)
		tb.beacons[epoch] = genesis

		if tb.tortoiseBeaconDB != nil {
			if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epoch, genesis); err != nil {
				tb.Log.With().Error("Failed to write tortoise beacon to DB",
					log.Uint64("epoch_id", uint64(epoch)),
					log.String("beacon", genesis.String()))
			}
		}
	}
}

func (tb *TortoiseBeacon) cleanup() {
	// TODO(nkryuchkov): implement a better solution, consider https://github.com/golang/go/issues/20135
	tb.beaconsMu.Lock()
	defer tb.beaconsMu.Unlock()

	for e := range tb.beacons {
		// TODO(nkryuchkov): https://github.com/spacemeshos/go-spacemesh/pull/2267#discussion_r662255874
		if tb.epochIsOutdated(e) {
			delete(tb.beacons, e)
		}
	}
}

func (tb *TortoiseBeacon) epochIsOutdated(epoch types.EpochID) bool {
	return tb.currentEpoch()-epoch > cleanupEpochs
}

// listens to new layers.
func (tb *TortoiseBeacon) listenLayers(ctx context.Context) {
	tb.Log.With().Info("Starting listening layers")

	for {
		select {
		case <-tb.CloseChannel():
			return
		case layer := <-tb.layerTicker:
			tb.Log.With().Info("Received tick", layer)

			go tb.handleLayer(ctx, layer)
		}
	}
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new CPs.
func (tb *TortoiseBeacon) handleLayer(ctx context.Context, layer types.LayerID) {
	tb.layerMu.Lock()
	if layer.After(tb.lastLayer) {
		tb.Log.With().Debug("Updating layer",
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

	tb.Log.With().Info("Layer is first in epoch, proceeding",
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
		log.String("wait_time", tb.waitAfterEpochStart.String()))

	epochStartTimer := time.NewTimer(tb.waitAfterEpochStart)

	select {
	case <-tb.CloseChannel():
		if !epochStartTimer.Stop() {
			<-epochStartTimer.C
		}
	case <-epochStartTimer.C:
		tb.handleEpoch(ctx, epoch)
	}
}

func (tb *TortoiseBeacon) handleEpoch(ctx context.Context, epoch types.EpochID) {
	// TODO: check when epoch started, adjust waiting time for this timestamp
	if epoch.IsGenesis() {
		tb.Log.With().Debug("not starting tortoise beacon since we are in genesis epoch",
			log.Uint64("epoch_id", uint64(epoch)))

		return
	}

	tb.Log.With().Info("Handling epoch",
		log.Uint64("epoch_id", uint64(epoch)))

	tb.proposalChansMu.Lock()
	if epoch > 0 {
		// close channel for previous epoch
		tb.closeProposalChannel(epoch - 1)
	}
	ch := tb.getOrCreateProposalChannel(epoch)
	tb.proposalChansMu.Unlock()

	go tb.readProposalMessagesLoop(ctx, ch)

	tb.runProposalPhase(ctx, epoch)
	tb.runConsensusPhase(ctx, epoch)

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	if err := tb.calcBeacon(epoch); err != nil {
		tb.Log.With().Error("Failed to calculate beacon",
			log.Uint64("epoch_id", uint64(epoch)),
			log.Err(err))
	}

	tb.Log.With().Debug("Finished handling epoch",
		log.Uint64("epoch_id", uint64(epoch)))
}

func (tb *TortoiseBeacon) readProposalMessagesLoop(ctx context.Context, ch chan *proposalMessageWithReceiptData) {
	for {
		select {
		case <-tb.CloseChannel():
			return

		case em := <-ch:
			if em == nil {
				return
			}

			if err := tb.handleProposalMessage(em.message, em.receivedTime); err != nil {
				tb.Log.With().Error("Failed to handle proposal message",
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
	tb.Log.With().Debug("Starting proposal phase",
		log.Uint64("epoch_id", uint64(epoch)))

	var cancel func()
	ctx, cancel = context.WithTimeout(ctx, tb.proposalDuration)
	defer cancel()

	go func() {
		tb.Log.With().Debug("Starting proposal message sender",
			log.Uint64("epoch_id", uint64(epoch)))

		if err := tb.proposalPhaseImpl(ctx, epoch); err != nil {
			tb.Log.With().Error("Failed to send proposal message",
				log.Uint64("epoch_id", uint64(epoch)),
				log.Err(err))
		}

		tb.Log.With().Debug("Proposal message sender finished",
			log.Uint64("epoch_id", uint64(epoch)))
	}()

	select {
	case <-tb.CloseChannel():
	case <-ctx.Done():
		tb.markProposalPhaseFinished(epoch)

		tb.Log.With().Debug("Proposal phase finished",
			log.Uint64("epoch_id", uint64(epoch)))
	}
}

func (tb *TortoiseBeacon) proposalPhaseImpl(ctx context.Context, epoch types.EpochID) error {
	proposedSignature, err := tb.getSignedProposal(epoch)
	if err != nil {
		return fmt.Errorf("calculate signed proposal: %w", err)
	}

	tb.Log.With().Debug("Calculated proposal signature",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("signature", util.Bytes2Hex(proposedSignature)))

	epochWeight, _, err := tb.atxDB.GetEpochWeight(epoch)
	if err != nil {
		return fmt.Errorf("get epoch weight: %w", err)
	}

	passes, err := tb.proposalPassesEligibilityThreshold(proposedSignature, epochWeight)
	if err != nil {
		return fmt.Errorf("proposalPassesEligibilityThreshold: %w", err)
	}

	if !passes {
		tb.Log.With().Debug("Proposal to be sent doesn't pass threshold",
			log.Uint64("epoch_id", uint64(epoch)),
			log.String("proposal", util.Bytes2Hex(proposedSignature)),
			log.Uint64("weight", epochWeight))
		// proposal is not sent
		return nil
	}

	tb.Log.With().Debug("Proposal to be sent passes threshold",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("proposal", util.Bytes2Hex(proposedSignature)),
		log.Uint64("weight", epochWeight))

	// concat them into a single proposal message
	m := ProposalMessage{
		EpochID:      epoch,
		MinerID:      tb.minerID,
		VRFSignature: proposedSignature,
	}

	tb.Log.With().Debug("Going to send proposal",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBProposalProtocol, m); err != nil {
		return fmt.Errorf("broadcast proposal message: %w", err)
	}

	tb.Log.With().Info("Sent proposal",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("message", m.String()))

	tb.validProposalsMu.Lock()

	if _, ok := tb.validProposals[epoch]; !ok {
		tb.validProposals[epoch] = make(map[proposal]struct{})
	}

	tb.validProposals[epoch][util.Bytes2Hex(proposedSignature)] = struct{}{}

	tb.validProposalsMu.Unlock()

	return nil
}

func (tb *TortoiseBeacon) runConsensusPhase(ctx context.Context, epoch types.EpochID) {
	tb.Log.With().Debug("Starting consensus phase",
		log.Uint64("epoch_id", uint64(epoch)))

	firstVoteCtx, cancel := context.WithTimeout(ctx, tb.firstVotingRoundDuration)
	defer cancel()

	go func() {
		tb.Log.With().Debug("Starting first voting message sender",
			log.Uint64("epoch_id", uint64(epoch)))

		if err := tb.sendVotes(firstVoteCtx, epoch, firstRound); err != nil {
			tb.Log.With().Error("Failed to send first voting message",
				log.Uint64("epoch_id", uint64(epoch)),
				log.Err(err))
		}

		tb.Log.With().Debug("First voting message sender finished",
			log.Uint64("epoch_id", uint64(epoch)))
	}()

	select {
	case <-tb.CloseChannel():
		return
	case <-ctx.Done():
		return
	case <-firstVoteCtx.Done():
		break
	}

	tb.Log.With().Debug("Starting following voting message sender",
		log.Uint64("epoch_id", uint64(epoch)))

	// For K rounds: In each round that lasts δ, wait for proposals to come in.
	// For next rounds,
	// wait for δ time, and construct a message that points to all messages from previous round received by δ.
	// rounds 1 to K
	ticker := time.NewTicker(tb.votingRoundDuration + tb.weakCoinRoundDuration)
	defer ticker.Stop()

	tb.sendFollowingVotesLoopIteration(ctx, epoch, firstRound+1)

	for round := firstRound + 2; round <= tb.lastPossibleRound(); round++ {
		select {
		case <-ticker.C:
			tb.sendFollowingVotesLoopIteration(ctx, epoch, round)
		case <-tb.CloseChannel():
			return
		case <-ctx.Done():
			return
		}
	}

	tb.Log.With().Debug("Following voting message sender finished",
		log.Uint64("epoch_id", uint64(epoch)))

	tb.waitAfterLastRoundStarted()
	tb.weakCoin.OnRoundFinished(epoch, tb.lastPossibleRound())

	tb.Log.With().Debug("Consensus phase finished",
		log.Uint64("epoch_id", uint64(epoch)))
}

func (tb *TortoiseBeacon) markProposalPhaseFinished(epoch types.EpochID) {
	finishedAt := time.Now()

	tb.proposalPhaseFinishedTimestampsMu.Lock()
	tb.proposalPhaseFinishedTimestamps[epoch] = finishedAt
	tb.proposalPhaseFinishedTimestampsMu.Unlock()

	tb.Debug("marked proposal phase for epoch %v finished at %v", epoch, finishedAt.String())
}

func (tb *TortoiseBeacon) receivedBeforeProposalPhaseFinished(epoch types.EpochID, receivedAt time.Time) bool {
	tb.proposalPhaseFinishedTimestampsMu.RLock()
	finishedAt, ok := tb.proposalPhaseFinishedTimestamps[epoch]
	tb.proposalPhaseFinishedTimestampsMu.RUnlock()

	tb.Debug("checking if timestamp %v was received before proposal phase finished in epoch %v, is phase finished: %v, finished at: %v", receivedAt.String(), epoch, ok, finishedAt.String())

	return !ok || receivedAt.Before(finishedAt)
}

func (tb *TortoiseBeacon) sendFollowingVotesLoopIteration(ctx context.Context, epoch types.EpochID, round types.RoundID) {
	tb.Log.With().Debug("Going to handle voting round",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)))

	go func(epoch types.EpochID, round types.RoundID) {
		if round > firstRound+1 {
			tb.weakCoin.OnRoundFinished(epoch, round-1)
		}

		if err := tb.sendVotes(ctx, epoch, round); err != nil {
			tb.Log.With().Error("Failed to send voting messages",
				log.Uint64("epoch_id", uint64(epoch)),
				log.Uint64("round_id", uint64(round)),
				log.Err(err))
		}
	}(epoch, round)

	go func(epoch types.EpochID, round types.RoundID) {
		t := time.NewTimer(tb.votingRoundDuration)
		defer t.Stop()

		select {
		case <-t.C:
			break
		case <-tb.CloseChannel():
			return
		case <-ctx.Done():
			return
		}

		tb.weakCoin.OnRoundStarted(epoch, round)

		// TODO(nkryuchkov):
		// should be published only after we should have received them
		if err := tb.weakCoin.PublishProposal(ctx, epoch, round); err != nil {
			tb.Log.With().Error("Failed to publish weak coin proposal",
				log.Uint64("epoch_id", uint64(epoch)),
				log.Uint64("round_id", uint64(round)),
				log.Err(err))
		}
	}(epoch, round)
}

func (tb *TortoiseBeacon) sendVotes(ctx context.Context, epoch types.EpochID, round types.RoundID) error {
	tb.setCurrentRound(epoch, round)

	if round == firstRound {
		return tb.sendProposalVote(ctx, epoch)
	}

	return tb.sendVotesDifference(ctx, epoch, round)
}

func (tb *TortoiseBeacon) sendProposalVote(ctx context.Context, epoch types.EpochID) error {
	// round 1, send hashed proposal
	// create a voting message that references all seen proposals within δ time frame and send it
	return tb.sendFirstRoundVote(ctx, epoch, tb.calcVotesFromProposals(epoch))
}

func (tb *TortoiseBeacon) sendVotesDifference(ctx context.Context, epoch types.EpochID, round types.RoundID) error {
	// next rounds, send vote
	// construct a message that points to all messages from previous round received by δ
	ownCurrentRoundVotes, err := tb.calcVotes(epoch, round)
	if err != nil {
		return fmt.Errorf("calculate votes: %w", err)
	}

	return tb.sendFollowingVote(ctx, epoch, round, ownCurrentRoundVotes)
}

func (tb *TortoiseBeacon) sendFirstRoundVote(ctx context.Context, epoch types.EpochID, vote firstRoundVotes) error {
	valid := make([][]byte, 0)
	potentiallyValid := make([][]byte, 0)

	for _, v := range vote.ValidVotes {
		valid = append(valid, util.Hex2Bytes(v))
	}

	for _, v := range vote.PotentiallyValidVotes {
		potentiallyValid = append(potentiallyValid, util.Hex2Bytes(v))
	}

	mb := FirstVotingMessageBody{
		MinerID:                   tb.minerID,
		ValidProposals:            valid,
		PotentiallyValidProposals: potentiallyValid,
	}

	sig, err := tb.signMessage(mb)
	if err != nil {
		return fmt.Errorf("signMessage: %w", err)
	}

	m := FirstVotingMessage{
		FirstVotingMessageBody: mb,
		Signature:              sig,
	}

	tb.Log.With().Debug("Going to send first round vote",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(1)),
		log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBFirstVotingProtocol, m); err != nil {
		return fmt.Errorf("sendToGossip: %w", err)
	}

	return nil
}

func (tb *TortoiseBeacon) sendFollowingVote(ctx context.Context, epoch types.EpochID, round types.RoundID, ownCurrentRoundVotes votesSetPair) error {
	bitVector := tb.encodeVotes(ownCurrentRoundVotes, tb.firstRoundOutcomingVotes[epoch])

	mb := FollowingVotingMessageBody{
		MinerID:        tb.minerID,
		EpochID:        epoch,
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

	tb.Log.With().Debug("Going to send following round vote",
		log.Uint64("epoch_id", uint64(epoch)),
		log.Uint64("round_id", uint64(round)),
		log.String("message", m.String()))

	if err := tb.sendToGossip(ctx, TBFollowingVotingProtocol, m); err != nil {
		return fmt.Errorf("broadcast voting message: %w", err)
	}

	return nil
}

func (tb *TortoiseBeacon) setCurrentRound(epoch types.EpochID, round types.RoundID) {
	tb.currentRoundsMu.Lock()
	defer tb.currentRoundsMu.Unlock()

	tb.currentRounds[epoch] = round
}

func (tb *TortoiseBeacon) voteWeight(pk nodeID, epochID types.EpochID) (uint64, error) {
	// TODO(nkryuchkov): enable
	enabled := false
	if !enabled {
		return 1, nil
	}

	nodeID := types.NodeID{
		Key:          pk,
		VRFPublicKey: nil,
	}

	atxID, err := tb.atxDB.GetNodeAtxIDForEpoch(nodeID, epochID-1)
	if err != nil {
		return 0, fmt.Errorf("atx ID for epoch: %w", err)
	}

	atx, err := tb.atxDB.GetAtxHeader(atxID)
	if err != nil {
		return 0, fmt.Errorf("atx header: %w", err)
	}

	return atx.GetWeight(), nil
}

// Each smesher partitions the valid proposals received in the previous epoch into three sets:
// - Timely proposals: received up to δ after the end of the previous epoch.
// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
// - Late proposals: more than 2δ after the end of the previous epoch.
// Note that honest users cannot disagree on timing by more than δ,
// so if a proposal is timely for any honest user,
// it cannot be late for any honest user (and vice versa).
func (tb *TortoiseBeacon) lastPossibleRound() types.RoundID {
	return types.RoundID(tb.config.RoundsNumber)
}

func (tb *TortoiseBeacon) waitAfterLastRoundStarted() {
	// Last round + next round for timely messages + next round for delayed messages (late messages may be ignored).
	const roundsToWait = 1
	timeToWait := roundsToWait * (tb.votingRoundDuration + tb.weakCoinRoundDuration)

	timer := time.NewTimer(timeToWait)
	defer timer.Stop()

	select {
	case <-tb.CloseChannel():
	case <-timer.C:
	}
}

func (tb *TortoiseBeacon) votingThreshold(epochWeight uint64) int {
	return int(tb.config.Theta * float64(epochWeight))
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
								tb.q,
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

// TODO: Consider having a generic function for probabilities.
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
	tb.Log.With().Debug("Calculated signature",
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("proposal", util.Bytes2Hex(p)),
		log.String("signature", util.Bytes2Hex(signature)))

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
		Epoch  uint64
	}{
		Prefix: proposalPrefix,
		Epoch:  uint64(epoch),
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

	tb.Log.With().Debug("Checking proposal for ATX threshold",
		log.String("proposal", proposalInt.String()),
		log.String("threshold", threshold.String()))

	return proposalInt.Cmp(threshold) == -1, nil
}
