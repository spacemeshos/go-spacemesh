package tortoisebeacon

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/weakcoin"
)

const (
	protoName       = "TORTOISE_BEACON_PROTOCOL"
	cleanupInterval = 30 * time.Second
	cleanupEpochs   = 1000
	firstRound      = types.RoundID(1)
)

// Tortoise Beacon errors.
var (
	ErrUnknownMessageType  = errors.New("unknown message type")
	ErrBeaconNotCalculated = errors.New("beacon is not calculated for this epoch")
)

type epochATXGetter interface {
	GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID)
}

type broadcaster interface {
	Broadcast(channel string, data []byte) error
}

type tortoiseBeaconDB interface {
	GetTortoiseBeacon(epochID types.EpochID) (types.Hash32, bool)
	SetTortoiseBeacon(epochID types.EpochID, beacon types.Hash32) error
}

type epochRoundPair struct {
	EpochID types.EpochID
	Round   types.RoundID
}

type (
	hashSet       = map[types.Hash32]struct{}
	votesPerPK    = map[p2pcrypto.PublicKey]votesSetPair
	votesPerRound = map[epochRoundPair]votesPerPK
	ownVotes      = map[epochRoundPair]votesSetPair
	votesCountMap = map[types.Hash32]int
	proposalsMap  = map[types.EpochID]hashSet
)

// TortoiseBeacon represents Tortoise Beacon.
type TortoiseBeacon struct {
	util.Closer
	log.Log

	config Config

	net              broadcaster
	epochATXGetter   epochATXGetter
	tortoiseBeaconDB tortoiseBeaconDB
	weakCoin         weakcoin.WeakCoin

	layerMu   sync.RWMutex
	lastLayer types.LayerID

	layerTicker  chan types.LayerID
	networkDelta time.Duration

	currentRoundsMu sync.RWMutex
	currentRounds   map[types.EpochID]types.RoundID

	timelyProposalsMu sync.RWMutex
	timelyProposals   proposalsMap

	delayedProposalsMu sync.RWMutex
	delayedProposals   proposalsMap

	votesMu         sync.RWMutex
	incomingVotes   votesPerRound // 1st round - votes, other rounds - diff
	votesCache      votesPerRound // all rounds - votes
	ownVotes        ownVotes      // all rounds - own votes
	votesCountCache map[epochRoundPair]map[types.Hash32]int

	beaconsMu sync.RWMutex
	beacons   map[types.EpochID]types.Hash32
	// beaconsReady indicates if beacons are ready.
	// If a beacon for an epoch becomes ready, channel for this epoch becomes closed.
	beaconsReady map[types.EpochID]chan struct{}

	seenEpochs map[types.EpochID]struct{}

	backgroundWG sync.WaitGroup
	startedOnce  sync.Once
	started      chan struct{}
}

// New returns a new TortoiseBeacon.
func New(
	conf Config,
	net broadcaster,
	epochATXGetter epochATXGetter,
	tortoiseBeaconDB tortoiseBeaconDB,
	weakCoin weakcoin.WeakCoin,
	layerTicker chan types.LayerID,
	logger log.Log,
) *TortoiseBeacon {
	return &TortoiseBeacon{
		Log:              logger,
		Closer:           util.NewCloser(),
		config:           conf,
		net:              net,
		epochATXGetter:   epochATXGetter,
		tortoiseBeaconDB: tortoiseBeaconDB,
		weakCoin:         weakCoin,
		layerTicker:      layerTicker,
		networkDelta:     time.Duration(conf.WakeupDelta) * time.Second,
		currentRounds:    make(map[types.EpochID]types.RoundID),
		timelyProposals:  make(map[types.EpochID]hashSet),
		delayedProposals: make(map[types.EpochID]hashSet),
		incomingVotes:    make(votesPerRound),
		votesCache:       make(votesPerRound),
		ownVotes:         make(ownVotes),
		votesCountCache:  make(map[epochRoundPair]map[types.Hash32]int),
		beacons:          make(map[types.EpochID]types.Hash32),
		beaconsReady:     make(map[types.EpochID]chan struct{}),
		seenEpochs:       make(map[types.EpochID]struct{}),
		started:          make(chan struct{}),
	}
}

// Start starts listening for layers and outputs.
func (tb *TortoiseBeacon) Start() error {
	tb.Log.Info("Starting %v with the following config: %+v", protoName, tb.config)

	tb.initGenesisBeacons()

	tb.backgroundWG.Add(1)

	go func() {
		defer tb.backgroundWG.Done()

		tb.listenLayers()
	}()

	tb.backgroundWG.Add(1)

	go func() {
		defer tb.backgroundWG.Done()

		tb.cleanupLoop()
	}()

	return nil
}

func (tb *TortoiseBeacon) initGenesisBeacons() {
	genesisBeacon := types.Hash32{} // zeros

	closedCh := make(chan struct{})
	close(closedCh)

	epoch := types.EpochID(0)
	for ; epoch.IsGenesis(); epoch++ {
		tb.beacons[epoch] = genesisBeacon
		tb.beaconsReady[epoch] = closedCh
		tb.seenEpochs[epoch] = struct{}{}
	}

	tb.beaconsReady[epoch] = make(chan struct{}) // get the next epoch ready
}

// Close closes TortoiseBeacon.
func (tb *TortoiseBeacon) Close() error {
	tb.Log.Info("Closing %v", protoName)
	tb.Closer.Close()
	tb.backgroundWG.Wait() // Wait until background goroutines finish

	return nil
}

// Get returns a Tortoise Beacon value as types.Hash32 for a certain epoch.
// TODO(nkryuchkov): Remove either Get or GetBeacon.
func (tb *TortoiseBeacon) Get(epochID types.EpochID) (types.Hash32, error) {
	if tb.tortoiseBeaconDB != nil {
		if val, ok := tb.tortoiseBeaconDB.GetTortoiseBeacon(epochID); ok {
			return val, nil
		}
	}

	tb.beaconsMu.RLock()
	beacon, ok := tb.beacons[epochID]
	tb.beaconsMu.RUnlock()

	if !ok {
		return types.Hash32{}, ErrBeaconNotCalculated
	}

	if tb.tortoiseBeaconDB != nil {
		if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epochID, beacon); err != nil {
			return types.Hash32{}, fmt.Errorf("update beacon in DB: %w", err)
		}
	}

	return beacon, nil
}

// GetBeacon returns a Tortoise Beacon value as []byte for a certain epoch.
func (tb *TortoiseBeacon) GetBeacon(epochNumber types.EpochID) []byte {
	if err := tb.Wait(epochNumber); err != nil {
		return nil
	}

	v, err := tb.Get(epochNumber)
	if err != nil {
		return nil
	}

	return v.Bytes()
}

// Wait waits until beacon for this epoch becomes ready.
func (tb *TortoiseBeacon) Wait(epochID types.EpochID) error {
	tb.waitUntilStarted()

	if tb.tortoiseBeaconDB != nil {
		if _, ok := tb.tortoiseBeaconDB.GetTortoiseBeacon(epochID); ok {
			return nil
		}
	}

	tb.beaconsMu.RLock()
	ch, ok := tb.beaconsReady[epochID]
	tb.beaconsMu.RUnlock()

	if !ok {
		return ErrBeaconNotCalculated
	}

	<-ch

	return nil
}

func (tb *TortoiseBeacon) waitUntilStarted() {
	select {
	case <-tb.CloseChannel():
		return
	case <-tb.started:
		return
	}
}

func (tb *TortoiseBeacon) setStarted() {
	tb.startedOnce.Do(func() {
		close(tb.started)
	})
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

func (tb *TortoiseBeacon) cleanup() {
	// TODO(nkryuchkov): implement a better solution, consider https://github.com/golang/go/issues/20135
	for e := range tb.beacons {
		if tb.epochIsOutdated(e) {
			delete(tb.beacons, e)
			delete(tb.beaconsReady, e)
		}
	}
}

func (tb *TortoiseBeacon) epochIsOutdated(epoch types.EpochID) bool {
	tb.layerMu.Lock()
	lastEpoch := tb.lastLayer.GetEpoch()
	tb.layerMu.Unlock()

	return lastEpoch-epoch > cleanupEpochs
}

// listens to new layers.
func (tb *TortoiseBeacon) listenLayers() {
	for {
		select {
		case <-tb.CloseChannel():
			tb.setStarted()

			return

		case layer := <-tb.layerTicker:
			tb.Log.With().Info("Received tick",
				log.Uint64("layer", uint64(layer)))

			go tb.handleLayer(layer)
		}
	}
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new CPs.
func (tb *TortoiseBeacon) handleLayer(layer types.LayerID) {
	tb.layerMu.Lock()
	if layer > tb.lastLayer {
		tb.lastLayer = layer
	}

	tb.layerMu.Unlock()

	epoch := layer.GetEpoch()
	tb.Log.With().Info("tortoise beacon got tick",
		log.Uint64("layer", uint64(layer)),
		log.Uint64("epoch", uint64(epoch)))

	tb.handleEpoch(epoch)
}

func (tb *TortoiseBeacon) handleEpoch(epoch types.EpochID) {
	if epoch.IsGenesis() {
		tb.Log.With().Info("not starting tortoise beacon since we are in genesis epoch",
			log.Uint64("epoch", uint64(epoch)))

		return
	}

	tb.Log.With().Info("Handling epoch",
		log.Uint64("epoch", uint64(epoch)))

	tb.beaconsMu.Lock()
	if _, ok := tb.seenEpochs[epoch]; ok {
		tb.beaconsMu.Unlock()

		// Already handling this epoch
		return
	}

	tb.seenEpochs[epoch] = struct{}{}

	tb.setStarted()

	if _, ok := tb.beaconsReady[epoch]; !ok {
		tb.beaconsReady[epoch] = make(chan struct{})
	}
	tb.beaconsMu.Unlock()

	tb.Log.With().Info("Starting round ticker",
		log.Uint64("epoch", uint64(epoch)))

	if err := tb.runProposalPhase(epoch); err != nil {
		tb.Log.With().Error("Failed to send proposal",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))

		return
	}

	if err := tb.runConsensusPhase(epoch); err != nil {
		tb.Log.With().Error("Failed to run consensus phase",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))
	}

	// K rounds passed
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	tb.calcBeacon(epoch)
}

func (tb *TortoiseBeacon) runConsensusPhase(epoch types.EpochID) error {
	// For K rounds: In each round that lasts δ, wait for proposals to come in.
	if err := tb.sendProposalVote(epoch); err != nil {
		tb.Log.With().Error("Failed to send first voting message",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))

		return err
	}

	// rounds 1 to K
	ticker := time.NewTicker(tb.networkDelta)
	defer ticker.Stop()

	// For next rounds,
	// wait for δ time, and construct a message that points to all messages from previous round received by δ.
	for round := firstRound + 1; round <= tb.lastPossibleRound(); round++ {
		select {
		case <-ticker.C:
			if err := tb.sendVotesDelta(epoch, round); err != nil {
				tb.Log.With().Error("Failed to send voting messages",
					log.Uint64("epoch", uint64(epoch)),
					log.Uint64("round", uint64(round)),
					log.Err(err))

				return err
			}

		case <-tb.CloseChannel():
			return nil
		}
	}

	return nil
}

func (tb *TortoiseBeacon) runProposalPhase(epoch types.EpochID) error {
	// take all ATXs received in last epoch (i - 1)
	atxList := ATXIDList(tb.epochATXGetter.GetEpochAtxs(epoch - 1))

	// concat them into a single proposal message
	m := NewProposalMessage(epoch, atxList)

	serializedMessage, err := types.InterfaceToBytes(m)
	if err != nil {
		return fmt.Errorf("serialize proposal message: %w", err)
	}

	tb.Log.With().Debug("Serialized proposal message",
		log.String("message", string(serializedMessage)))

	if err := tb.net.Broadcast(TBProposalProtocol, serializedMessage); err != nil {
		return fmt.Errorf("broadcast proposal message: %w", err)
	}

	proposalsHash := atxList.Hash()

	tb.timelyProposalsMu.Lock()

	if _, ok := tb.timelyProposals[epoch]; !ok {
		tb.timelyProposals[epoch] = make(map[types.Hash32]struct{})
	}

	tb.timelyProposals[epoch][proposalsHash] = struct{}{}

	tb.timelyProposalsMu.Unlock()

	if err := tb.weakCoin.PublishProposal(epoch, 1); err != nil {
		tb.Log.With().Error("Failed to publish weak coin message",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))
	}

	return nil
}

func (tb *TortoiseBeacon) sendProposalVote(epoch types.EpochID) error {
	tb.setCurrentRound(epoch, firstRound)
	// round 1, send hashed proposal
	// create a voting message that references all seen proposals within δ time frame and send it
	votesFor, votesAgainst := tb.calcVotesFromProposals(epoch)

	return tb.sendVote(epoch, firstRound, votesFor, votesAgainst)
}

func (tb *TortoiseBeacon) sendVotesDelta(epoch types.EpochID, round types.RoundID) error {
	tb.setCurrentRound(epoch, round)
	// next rounds, send vote
	// construct a message that points to all messages from previous round received by δ
	votesFor, votesAgainst := tb.calcVotesDelta(epoch, round)

	return tb.sendVote(epoch, round, votesFor, votesAgainst)
}

func (tb *TortoiseBeacon) sendVote(
	epoch types.EpochID,
	round types.RoundID,
	votesFor,
	votesAgainst hashList,
) error {
	m := NewVotingMessage(epoch, round, votesFor, votesAgainst)

	serializedMessage, err := types.InterfaceToBytes(m)
	if err != nil {
		return fmt.Errorf("serialize voting message: %w", err)
	}

	tb.Log.With().Debug("Serialized voting message",
		log.String("message", string(serializedMessage)))

	if err := tb.net.Broadcast(TBVotingProtocol, serializedMessage); err != nil {
		return fmt.Errorf("broadcast voting message: %w", err)
	}

	if err := tb.weakCoin.PublishProposal(epoch, round); err != nil {
		tb.Log.With().Error("Failed to publish weak coin message",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))
	}

	return nil
}

func (tb *TortoiseBeacon) setCurrentRound(epoch types.EpochID, round types.RoundID) {
	tb.currentRoundsMu.Lock()
	tb.currentRounds[epoch] = round
	tb.currentRoundsMu.Unlock()
}

func (tb *TortoiseBeacon) voteWeight(pk p2pcrypto.PublicKey) int {
	// TODO(nkryuchkov): implement
	return 1
}

// Each smesher partitions the valid proposals received in the previous epoch into three sets:
// - Timely proposals: received up to δ after the end of the previous epoch.
// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
// - Late proposals: more than 2δ after the end of the previous epoch.
// Note that honest users cannot disagree on timing by more than δ,
// so if a proposal is timely for any honest user,
// it cannot be late for any honest user (and vice versa).
//
// K is the last round (counting starts from 1).
// That means:
// Messages from round K received in round K + 1 are timely.
// Messages from round K received in round K + 2 are delayed.
// Messages from round K received in round K + 3 are late.
// Therefore, counting more than K + 3 rounds is not needed.
func (tb *TortoiseBeacon) lastPossibleRound() types.RoundID {
	return types.RoundID(tb.config.RoundsNumber) + 3
}

func (tb *TortoiseBeacon) threshold() int {
	return tb.config.Theta * tb.config.TAve
}
