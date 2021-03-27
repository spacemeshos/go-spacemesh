package tortoisebeacon

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	protoName       = "TORTOISE_BEACON_PROTOCOL"
	cleanupInterval = 30 * time.Second
	cleanupEpochs   = 1000
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

type weakCoin interface {
	WeakCoin(epoch types.EpochID, round uint64) bool
}

type tortoiseBeaconDB interface {
	GetTortoiseBeacon(epochID types.EpochID) (types.Hash32, bool)
	SetTortoiseBeacon(epochID types.EpochID, beacon types.Hash32) error
}

type epochRoundPair struct {
	EpochID types.EpochID
	Round   uint64
}

type hashSet = map[types.Hash32]struct{}
type votesMap = map[epochRoundPair]hashSet

// TortoiseBeacon represents Tortoise Beacon.
type TortoiseBeacon struct {
	Closer
	log.Log

	config Config

	net               broadcaster
	epochATXGetter    epochATXGetter
	tortoiseBeaconDB  tortoiseBeaconDB
	weakCoin          weakCoin
	weakCoinGenerator WeakCoinGenerator

	layerMu   sync.RWMutex
	lastLayer types.LayerID

	layerTicker  chan types.LayerID
	networkDelta time.Duration

	currentRoundsMu sync.RWMutex
	currentRounds   map[types.EpochID]uint64

	timelyProposalsMu   sync.RWMutex
	timelyProposalsList map[types.EpochID][][]types.ATXID

	delayedProposalsMu   sync.RWMutex
	delayedProposalsList map[types.EpochID][][]types.ATXID

	lateProposalsMu   sync.RWMutex
	lateProposalsList map[types.EpochID][][]types.ATXID

	votesMu      sync.RWMutex
	votesFor     votesMap
	votesAgainst votesMap

	beaconsMu sync.RWMutex
	beacons   map[types.EpochID]types.Hash32
	// beaconsReady indicates if beacons are ready.
	// If a beacon for an epoch becomes ready, channel for this epoch becomes closed.
	beaconsReady map[types.EpochID]chan struct{}

	backgroundWG sync.WaitGroup
}

// New returns a new TortoiseBeacon.
func New(
	conf Config,
	net broadcaster,
	epochATXGetter epochATXGetter,
	tortoiseBeaconDB tortoiseBeaconDB,
	weakCoin weakCoin,
	layerTicker chan types.LayerID,
	logger log.Log,
) *TortoiseBeacon {
	wcg := NewWeakCoinGenerator(defaultPrefix, defaultThreshold, net)

	return &TortoiseBeacon{
		Log:                  logger,
		Closer:               NewCloser(),
		config:               conf,
		net:                  net,
		epochATXGetter:       epochATXGetter,
		tortoiseBeaconDB:     tortoiseBeaconDB,
		weakCoin:             weakCoin,
		weakCoinGenerator:    wcg,
		layerTicker:          layerTicker,
		networkDelta:         time.Duration(conf.WakeupDelta) * time.Second,
		currentRounds:        make(map[types.EpochID]uint64),
		timelyProposalsList:  make(map[types.EpochID][][]types.ATXID),
		delayedProposalsList: make(map[types.EpochID][][]types.ATXID),
		lateProposalsList:    make(map[types.EpochID][][]types.ATXID),
		votesFor:             make(votesMap),
		votesAgainst:         make(votesMap),
		beacons:              make(map[types.EpochID]types.Hash32),
		beaconsReady:         make(map[types.EpochID]chan struct{}),
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

	for epoch := types.EpochID(0); epoch.IsGenesis(); epoch++ {
		tb.beacons[epoch] = genesisBeacon
		tb.beaconsReady[epoch] = closedCh
	}
}

// Close closes TortoiseBeacon.
func (tb *TortoiseBeacon) Close() error {
	tb.Log.Info("Closing %v", protoName)
	tb.Closer.Close()
	tb.backgroundWG.Wait() // Wait until background goroutines finish

	return nil
}

// Get returns a Tortoise Beacon value as types.Hash32 for a certain epoch.
// TODO(nkryuchkov): remove either Get or GetBeacon
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
			return types.Hash32{}, err
		}
	}

	return beacon, nil
}

// GetBeacon returns a Tortoise Beacon value as []byte for a certain epoch.
func (tb *TortoiseBeacon) GetBeacon(epochNumber types.EpochID) []byte {
	v, err := tb.Get(epochNumber)
	if err != nil {
		return nil
	}

	return v.Bytes()
}

// Wait waits until beacon for this epoch becomes ready.
func (tb *TortoiseBeacon) Wait(epochID types.EpochID) error {
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

	if _, ok := tb.beaconsReady[epoch]; ok {
		tb.beaconsMu.Unlock()

		// Already handling this epoch
		return
	}

	tb.beaconsReady[epoch] = make(chan struct{})
	tb.beaconsMu.Unlock()

	tb.Log.With().Info("Starting round ticker",
		log.Uint64("epoch", uint64(epoch)))

	tb.roundTicker(epoch)

	// K rounds passed
	tb.timelyProposalsMu.Lock()
	tb.Log.With().Info("Calculating beacon",
		log.Uint64("epoch", uint64(epoch)))

	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	beacon := tb.calculateBeacon(tb.votesFor, epoch)
	tb.timelyProposalsMu.Unlock()

	tb.Log.With().Info("Calculated beacon",
		log.Uint64("epoch", uint64(epoch)),
		log.String("beacon", beacon.String()))

	events.ReportCalculatedTortoiseBeacon(epoch, beacon.String())

	tb.beaconsMu.Lock()
	tb.beacons[epoch] = beacon
	close(tb.beaconsReady[epoch]) // indicate that value is ready
	tb.beaconsMu.Unlock()
}

func (tb *TortoiseBeacon) handleProposalMessage(m ProposalMessage) error {
	epoch := m.Epoch()

	mt := tb.classifyMessage(m, epoch)
	switch mt {
	case TimelyMessage:
		tb.Log.With().Debug("Received timely ProposalMessage",
			log.Uint64("epoch", uint64(m.Epoch())),
			log.String("message", m.String()))

		tb.timelyProposalsMu.Lock()
		tb.timelyProposalsList[epoch] = append(tb.timelyProposalsList[epoch], m.Proposals())
		tb.timelyProposalsMu.Unlock()

		return nil

	case DelayedMessage:
		tb.Log.With().Debug("Received delayed ProposalMessage",
			log.Uint64("epoch", uint64(m.Epoch())),
			log.String("message", m.String()))

		tb.delayedProposalsMu.Lock()
		tb.delayedProposalsList[epoch] = append(tb.delayedProposalsList[epoch], m.Proposals())
		tb.delayedProposalsMu.Unlock()

		return nil

	case LateMessage:
		tb.Log.With().Debug("Received late ProposalMessage",
			log.Uint64("epoch", uint64(m.Epoch())),
			log.String("message", m.String()))

		return nil

	default:
		return ErrUnknownMessageType
	}
}

func (tb *TortoiseBeacon) handleVotingMessage(m VotingMessage) error {
	epoch := m.Epoch()

	tb.currentRoundsMu.Lock()
	currentRound := tb.currentRounds[epoch]
	tb.currentRoundsMu.Unlock()

	pair := epochRoundPair{
		EpochID: epoch,
		Round:   currentRound,
	}

	mt := tb.classifyMessage(m, epoch)
	switch mt {
	case TimelyMessage:
		tb.Log.With().Debug("Received timely VotingMessage",
			log.Uint64("epoch", uint64(m.Epoch())),
			log.Uint64("round", m.Round()),
			log.String("message", m.String()))

		for _, hash := range m.VotesFor() {
			tb.votesMu.Lock()

			if _, ok := tb.votesFor[pair]; !ok {
				tb.votesFor[pair] = make(hashSet)
			}
			tb.votesFor[pair][hash] = struct{}{}

			if _, ok := tb.votesAgainst[pair]; !ok {
				tb.votesAgainst[pair] = make(hashSet)
			}
			tb.votesAgainst[pair][hash] = struct{}{}

			tb.votesMu.Unlock()
		}

		return nil

	case DelayedMessage:
		tb.Log.With().Debug("Received delayed VotingMessage",
			log.Uint64("epoch", uint64(m.Epoch())),
			log.Uint64("round", m.Round()),
			log.String("message", m.String()))

		return nil

	case LateMessage:
		tb.Log.With().Debug("Received late VotingMessage",
			log.Uint64("epoch", uint64(m.Epoch())),
			log.Uint64("round", m.Round()),
			log.String("message", m.String()))

		return nil

	default:
		return ErrUnknownMessageType
	}
}

func (tb *TortoiseBeacon) handleWeakCoinMessage(m WeakCoinMessage) error {
	// TODO(nkryuchkov): implement
	return nil
}

func (tb *TortoiseBeacon) classifyMessage(m message, epoch types.EpochID) MessageType {
	tb.currentRoundsMu.Lock()
	currentRound := tb.currentRounds[epoch]
	tb.currentRoundsMu.Unlock()

	round := uint64(0)
	if vm, ok := m.(VotingMessage); ok {
		round = vm.Round()
	}

	switch {
	case round >= currentRound-1:
		return TimelyMessage
	case round == currentRound-2:
		return DelayedMessage
	default:
		return LateMessage
	}
}

// For K rounds: In each round that lasts δ, wait for proposals to come in.
func (tb *TortoiseBeacon) roundTicker(epoch types.EpochID) {
	if err := tb.sendProposal(epoch); err != nil {
		tb.Log.With().Error("Failed to send proposal",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))

		return
	}

	// rounds 1 to K
	ticker := time.NewTicker(tb.networkDelta)
	defer ticker.Stop()

	tb.currentRoundsMu.Lock()
	tb.currentRounds[epoch] = 0
	tb.currentRoundsMu.Unlock()

	// Round 0 is already happened at this point, starting from round 1.
	// For next rounds,
	// wait for δ time, and construct a message that points to all messages from previous round received by δ.
	for i := uint64(1); i < tb.lastPossibleRound(); i++ {
		select {
		case <-ticker.C:
			if err := tb.sendVotingMessages(epoch, i); err != nil {
				tb.Log.With().Error("Failed to send voting messages",
					log.Uint64("epoch", uint64(epoch)),
					log.Uint64("i", i),
					log.Err(err))

				return
			}

		case <-tb.CloseChannel():
			return
		}
	}
}

func (tb *TortoiseBeacon) sendProposal(epoch types.EpochID) error {
	// round 0
	// take all ATXs received in last epoch (i -1)
	atxList := tb.epochATXGetter.GetEpochAtxs(epoch - 1)

	// concat them into a single proposal message
	m := NewProposalMessage(epoch, atxList)

	serializedMessage, err := types.InterfaceToBytes(m)
	if err != nil {
		return err
	}

	tb.Log.With().Debug("Serialized proposal message",
		log.String("message", string(serializedMessage)))

	if err := tb.net.Broadcast(TBProposalProtocol, serializedMessage); err != nil {
		return err
	}

	tb.timelyProposalsMu.Lock()
	tb.timelyProposalsList[epoch] = append(tb.timelyProposalsList[epoch], atxList)
	tb.timelyProposalsMu.Unlock()

	if err := tb.weakCoinGenerator.Publish(epoch, 0); err != nil {
		tb.Log.With().Error("Failed to publish weak coin message",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))
	}

	return nil
}

func (tb *TortoiseBeacon) sendVotingMessages(epoch types.EpochID, round uint64) error {
	tb.currentRoundsMu.Lock()
	tb.currentRounds[epoch]++
	currentRound := tb.currentRounds[epoch]
	tb.currentRoundsMu.Unlock()

	votesFor := make([]types.Hash32, 0)
	votesAgainst := make([]types.Hash32, 0)

	if round == 1 {
		// round 1, send hashed proposal
		// create a voting message that references all seen proposals within δ time frame and send it
		tb.timelyProposalsMu.RLock()
		timelyProposals := tb.timelyProposalsList[epoch]
		tb.timelyProposalsMu.RUnlock()

		tb.delayedProposalsMu.Lock()
		delayedProposals := tb.delayedProposalsList[epoch]
		tb.delayedProposalsMu.Unlock()

		for _, p := range timelyProposals {
			votesFor = append(votesFor, hashATXList(p))
		}

		for _, p := range delayedProposals {
			votesAgainst = append(votesAgainst, hashATXList(p))
		}
	} else {
		// next rounds, send vote
		// construct a message that points to all messages from previous round received by δ
		votesFor, votesAgainst = tb.calculateVotes(epoch, currentRound)
	}

	m := NewVotingMessage(epoch, currentRound, votesFor, votesAgainst)
	serializedMessage, err := types.InterfaceToBytes(m)
	if err != nil {
		return err
	}

	tb.Log.With().Debug("Serialized voting message",
		log.String("message", string(serializedMessage)))

	if err := tb.net.Broadcast(TBVotingProtocol, serializedMessage); err != nil {
		return err
	}

	if err := tb.weakCoinGenerator.Publish(epoch, currentRound); err != nil {
		tb.Log.With().Error("Failed to publish weak coin message",
			log.Uint64("epoch", uint64(epoch)),
			log.Err(err))
	}

	return nil
}

// TODO(nkryuchkov): send and handle votes diff instead of votes list
// TODO(nkryuchkov): refactor this
func (tb *TortoiseBeacon) calculateVotes(epoch types.EpochID, round uint64) (votesFor []types.Hash32, votesAgainst []types.Hash32) {
	votesForMap := make(map[types.Hash32]int)
	votesAgainstMap := make(map[types.Hash32]int)

	tb.timelyProposalsMu.Lock()

	for _, proposals := range tb.timelyProposalsList[epoch] {
		proposalsHash := hashATXList(proposals)

		countFor := 1
		countAgainst := 0
		for i := uint64(1); i < round; i++ {
			key := epochRoundPair{
				EpochID: epoch,
				Round:   i, // proposals from the previous round are needed
			}

			if _, ok := tb.votesFor[key][proposalsHash]; ok {
				countFor++
			}

			if _, ok := tb.votesAgainst[key][proposalsHash]; ok {
				countAgainst++
			}
		}

		votesForMap[proposalsHash] = countFor
		votesAgainstMap[proposalsHash] = countAgainst
	}

	for _, proposals := range tb.delayedProposalsList[epoch] {
		proposalsHash := hashATXList(proposals)

		countFor := 0
		countAgainst := 1
		for i := uint64(1); i < round; i++ {
			key := epochRoundPair{
				EpochID: epoch,
				Round:   i, // proposals from the previous round are needed
			}

			if _, ok := tb.votesFor[key][proposalsHash]; ok {
				countFor++
			}

			if _, ok := tb.votesAgainst[key][proposalsHash]; ok {
				countAgainst++
			}
		}

		if _, ok := votesForMap[proposalsHash]; !ok {
			votesForMap[proposalsHash] = countFor
		} else {
			votesForMap[proposalsHash] += countFor
		}

		if _, ok := votesAgainstMap[proposalsHash]; !ok {
			votesAgainstMap[proposalsHash] = countAgainst
		} else {
			votesAgainstMap[proposalsHash] += countAgainst
		}
	}

	tb.timelyProposalsMu.Unlock()

	votesFor = make([]types.Hash32, 0)
	votesAgainst = make([]types.Hash32, 0)

	for k, v := range votesForMap {
		if v > tb.threshold() || tb.weakCoin.WeakCoin(epoch, round) {
			votesFor = append(votesFor, k)
		}
	}

	for k, v := range votesAgainstMap {
		if v > tb.threshold() || !tb.weakCoin.WeakCoin(epoch, round) {
			votesAgainst = append(votesAgainst, k)
		}
	}

	return votesFor, votesAgainst
}

// Each smesher partitions the valid proposals received in the previous epoch into three sets:
// - Timely proposals: received up to δ after the end of the previous epoch.
// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
// - Late proposals: more than 2δ after the end of the previous epoch.
// Note that honest users cannot disagree on timing by more than δ,
// so if a proposal is timely for any honest user,
// it cannot be late for any honest user (and vice versa).
//
// K - 1 is the last round (counting starts from 0).
// That means:
// Messages from round K - 1 received in round K are timely.
// Messages from round K - 1 received in round K + 1 are delayed.
// Messages from round K - 1 received in round K + 2 are late.
// Therefore, counting more than K + 2 rounds is not needed.
func (tb *TortoiseBeacon) lastPossibleRound() uint64 {
	return tb.config.RoundsNumber + 2
}

func (tb *TortoiseBeacon) calculateBeacon(votes votesMap, epoch types.EpochID) types.Hash32 {
	hasher := sha256.New()

	allHashes := make([]types.Hash32, 0)

	for round := uint64(0); round < tb.config.RoundsNumber; round++ {
		epochRound := epochRoundPair{
			EpochID: epoch,
			Round:   round,
		}

		if hashList, ok := votes[epochRound]; ok {
			stringHashes := make([]string, 0, len(hashList))

			for hash := range hashList {
				allHashes = append(allHashes, hash)
				stringHashes = append(stringHashes, hash.String())
			}

			tb.Log.With().Info(fmt.Sprintf("Tortoise beacon hashes epoch %v round %v", epoch, round),
				log.Uint64("epoch_id", uint64(epoch)),
				log.Uint64("round", round),
				log.String("hashes", strings.Join(stringHashes, ", ")))
		}
	}
	//  AssertionError: all beacons in epoch 2 were not same, saw:
	// {'0x224451484fe1e61b08f634279600a8dcd82d3190a20cd3364a6023ac35be1b94': 46,
	//  '0x27c88a89b3071184aea7bedb8589e394a2eebce92e74145c9e129f6d9a3eb139': 2,
	//  '0xbfe29d6ef1861a13293f00b734fca93bc2c8f141e548e7fe2c97221b5b2cf28a': 3}

	sort.Slice(allHashes, func(i, j int) bool {
		return strings.Compare(allHashes[i].String(), allHashes[j].String()) == -1
	})

	stringHashes := make([]string, 0, len(allHashes))
	for _, hash := range allHashes {
		stringHashes = append(stringHashes, hash.String())
	}

	tb.Log.With().Info(fmt.Sprintf("Going to calculate tortoise beacon from this hash list epoch %v", epoch),
		log.Uint64("epoch_id", uint64(epoch)),
		log.String("hashes", strings.Join(stringHashes, ", ")))

	for _, hash := range allHashes {
		if _, err := hasher.Write(hash.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var res types.Hash32
	hasher.Sum(res[:0])

	return res
}

func (tb *TortoiseBeacon) threshold() int {
	return tb.config.Theta * tb.config.TAve
}

func hashATXList(atxList []types.ATXID) types.Hash32 {
	hasher := sha256.New()

	for _, id := range atxList {
		if _, err := hasher.Write(id.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var res types.Hash32
	hasher.Sum(res[:0])

	return res
}

// Closer adds the ability to close objects.
type Closer struct {
	channel chan struct{} // closeable go routines listen to this channel
}

// NewCloser creates a new (not closed) closer.
func NewCloser() Closer {
	return Closer{make(chan struct{})}
}

// Close signals all listening instances to close.
// Note: should be called only once.
func (closer *Closer) Close() {
	close(closer.channel)
}

// CloseChannel returns the channel to wait on for close signal.
func (closer *Closer) CloseChannel() chan struct{} {
	return closer.channel
}
