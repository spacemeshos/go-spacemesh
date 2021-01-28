package tortoisebeacon

import (
	"errors"
	"sync"
	"time"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	protoName       = "TORTOISE_BEACON_PROTOCOL"
	cleanupInterval = 30 * time.Second
	cleanupEpochs   = 20
)

var (
	ErrUnknownMessageType          = errors.New("unknown message type")
	ErrBeaconCalculationNotStarted = errors.New("beacon calculation for this epoch was not started")
)

type messageReceiver interface {
	Receive() Message
}

type messageSender interface {
	Send(Message) // TODO(nkryuchkov): sign message
}

type epochATXGetter interface {
	GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID)
}

type tortoiseBeaconDB interface {
	GetTortoiseBeacon(epochID types.EpochID) (types.Hash32, bool)
	SetTortoiseBeacon(epochID types.EpochID, beacon types.Hash32) error
}

type beaconCalculator interface {
	CalculateBeacon(map[EpochRoundPair][]types.Hash32) types.Hash32
	CountVotes(m VotingMessage) error
	CountBatchVotes(m BatchVotingMessage) error
}

type EpochRoundPair struct {
	EpochID types.EpochID
	Round   int
}

type TortoiseBeacon struct {
	hare.Closer
	log.Log

	config Config

	messageReceiver  messageReceiver
	messageSender    messageSender
	beaconCalculator beaconCalculator
	epochATXGetter   epochATXGetter
	tortoiseBeaconDB tortoiseBeaconDB

	layerMu   sync.RWMutex
	lastLayer types.LayerID

	layerTicker  chan types.LayerID
	networkDelta time.Duration

	currentRoundsMu sync.RWMutex
	currentRounds   map[types.EpochID]int

	timelyProposalsMu   sync.RWMutex
	timelyProposalsMap  map[types.Hash32][]types.ATXID // lookup by hash TODO((nkryuchkov): consider removing
	timelyProposalsList map[types.EpochID][][]types.ATXID

	// TODO(nkryuchkov): consider removing
	delayedProposalsMu   sync.RWMutex
	delayedProposalsList map[types.EpochID][][]types.ATXID

	// TODO(nkryuchkov): consider removing
	lateProposalsMu   sync.RWMutex
	lateProposalsList map[types.EpochID][][]types.ATXID

	votesMu sync.RWMutex
	votes   map[EpochRoundPair][]types.Hash32

	beaconsMu sync.RWMutex
	beacons   map[types.EpochID]types.Hash32
	// beaconsReady indicates if beacons are ready.
	//If a beacon for an epoch becomes ready, channel for this epoch becomes closed.
	beaconsReady map[types.EpochID]chan struct{}

	backgroundWG sync.WaitGroup
}

func New(
	conf Config,
	messageReceiver messageReceiver,
	messageSender messageSender,
	beaconCalculator beaconCalculator,
	epochATXGetter epochATXGetter,
	tortoiseBeaconDB tortoiseBeaconDB,
	layerTicker chan types.LayerID,
	logger log.Log,
) *TortoiseBeacon {
	return &TortoiseBeacon{
		Log:                  logger,
		config:               conf,
		messageReceiver:      messageReceiver,
		messageSender:        messageSender,
		beaconCalculator:     beaconCalculator,
		epochATXGetter:       epochATXGetter,
		tortoiseBeaconDB:     tortoiseBeaconDB,
		layerTicker:          layerTicker,
		networkDelta:         time.Duration(conf.WakeupDelta) * time.Second,
		currentRounds:        make(map[types.EpochID]int),
		timelyProposalsMap:   make(map[types.Hash32][]types.ATXID),
		timelyProposalsList:  make(map[types.EpochID][][]types.ATXID),
		delayedProposalsList: make(map[types.EpochID][][]types.ATXID),
		lateProposalsList:    make(map[types.EpochID][][]types.ATXID),
	}
}

// Start starts listening for layers and outputs.
func (tb *TortoiseBeacon) Start() error {
	tb.Log.Info("Starting %v", protoName)

	tb.backgroundWG.Add(1)
	go func() {
		defer tb.backgroundWG.Done()

		tb.listenLayers()
	}()

	tb.backgroundWG.Add(1)
	go func() {
		defer tb.backgroundWG.Done()

		if err := tb.listenMessages(); err != nil {
			tb.Log.With().Error("Error while listening for messages: %v", log.Err(err))
			return
		}
	}()

	tb.backgroundWG.Add(1)
	go func() {
		defer tb.backgroundWG.Done()

		tb.cleanupLoop()
	}()

	return nil
}

func (tb *TortoiseBeacon) Close() error {
	tb.Log.Info("Closing %v", protoName)
	tb.Closer.Close()
	tb.backgroundWG.Wait() // Wait until background goroutines finish

	return nil
}

func (tb *TortoiseBeacon) Get(epochID types.EpochID) (types.Hash32, error) {
	if val, ok := tb.tortoiseBeaconDB.GetTortoiseBeacon(epochID); ok {
		return val, nil
	}

	tb.beaconsMu.RLock()
	beacon, ok := tb.beacons[epochID]
	tb.beaconsMu.RUnlock()

	if !ok {
		return types.Hash32{}, ErrBeaconCalculationNotStarted
	}

	if err := tb.tortoiseBeaconDB.SetTortoiseBeacon(epochID, beacon); err != nil {
		return types.Hash32{}, err
	}

	return beacon, nil
}

// Wait waits until beacon for this epoch becomes ready.
func (tb *TortoiseBeacon) Wait(epochID types.EpochID) error {
	if _, ok := tb.tortoiseBeaconDB.GetTortoiseBeacon(epochID); ok {
		return nil
	}

	tb.beaconsMu.RLock()
	ch, ok := tb.beaconsReady[epochID]
	tb.beaconsMu.RUnlock()

	if !ok {
		return ErrBeaconCalculationNotStarted
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

func (tb *TortoiseBeacon) listenMessages() error {
	for {
		select {
		case <-tb.CloseChannel():
			return nil
		default:
			m := tb.messageReceiver.Receive()

			if err := tb.handleMessage(m); err != nil {
				return err
			}
		}
	}
}

// listens to new layers.
func (tb *TortoiseBeacon) listenLayers() {
	for {
		select {
		case <-tb.CloseChannel():
			return
		case layer := <-tb.layerTicker:
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
	tb.Debug("tortoise beacon got tick at layer %v epoch %v", layer, epoch)

	tb.handleEpoch(epoch)
}

func (tb *TortoiseBeacon) handleEpoch(epoch types.EpochID) {
	if epoch.IsGenesis() {
		tb.With().Info("not starting tortoise beacon since we are in genesis epoch", epoch)

		return
	}

	tb.beaconsMu.Lock()

	if _, ok := tb.beacons[epoch]; ok {
		tb.beaconsMu.Unlock()

		// Already handling this epoch
		return
	}

	tb.beaconsReady[epoch] = make(chan struct{})
	tb.beaconsMu.Unlock()

	// round 0
	// take all ATXs received in last epoch (i -1)
	atxList := tb.epochATXGetter.GetEpochAtxs(epoch - 1)

	// concat them into a single proposal message
	m := NewProposalMessage(epoch, atxList)
	tb.messageSender.Send(m)

	// rounds 1 to K
	tb.roundTicker(epoch)

	// K rounds passed
	tb.timelyProposalsMu.Lock()
	// After K rounds had passed, tally up votes for proposals using simple tortoise vote counting
	beacon := tb.beaconCalculator.CalculateBeacon(tb.votes)
	tb.timelyProposalsMu.Unlock()

	tb.beaconsMu.Lock()
	tb.beacons[epoch] = beacon
	close(tb.beaconsReady[epoch]) // indicate that value is ready
	tb.beaconsMu.Unlock()
}

func (tb *TortoiseBeacon) handleMessage(m Message) error {
	switch m := m.(type) {
	case ProposalMessage:
		return tb.handleProposalMessage(m)
	case VotingMessage:
		return tb.handleVotingMessage(m)
	case BatchVotingMessage:
		return tb.handleBatchVotingMessage(m)
	default:
		return ErrUnknownMessageType
	}
}

func (tb *TortoiseBeacon) handleProposalMessage(m ProposalMessage) error {
	epoch := m.Epoch()
	hash := m.Hash()

	mt := tb.classifyMessage(m, epoch)
	switch mt {
	case TimelyMessage:
		tb.timelyProposalsMu.Lock()

		tb.timelyProposalsMap[hash] = m.Proposals()
		tb.timelyProposalsList[epoch] = append(tb.timelyProposalsList[epoch], m.Proposals())

		tb.timelyProposalsMu.Unlock()

		return nil

	case DelayedMessage:
		tb.Log.Debug("Received delayed ProposalMessage, epoch: %v, proposals: %v", m.Epoch(), m.Proposals())
		return nil

	case LateMessage:
		tb.Log.Debug("Received late ProposalMessage, epoch: %v, proposals: %v", m.Epoch(), m.Proposals())
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

	pair := EpochRoundPair{
		EpochID: epoch,
		Round:   currentRound,
	}

	mt := tb.classifyMessage(m, epoch)
	switch mt {
	case TimelyMessage:
		tb.votesMu.Lock()
		tb.votes[pair] = append(tb.votes[pair], m.Hash())
		tb.votesMu.Unlock()

		return tb.beaconCalculator.CountVotes(m) // TODO(nkryuchkov): count votes properly

	case DelayedMessage:
		tb.Log.Debug("Received delayed VotingMessage, epoch: %v, proposals: %v, hash: %v",
			m.Epoch(), m.Round(), m.Hash())
		return nil

	case LateMessage:
		tb.Log.Debug("Received late VotingMessage, epoch: %v, round: %v, hash: %v",
			m.Epoch(), m.Round(), m.Hash())
		return nil

	default:
		return ErrUnknownMessageType
	}
}

func (tb *TortoiseBeacon) handleBatchVotingMessage(m BatchVotingMessage) error {
	epoch := m.Epoch()

	tb.currentRoundsMu.Lock()
	currentRound := tb.currentRounds[epoch]
	tb.currentRoundsMu.Unlock()

	pair := EpochRoundPair{
		EpochID: epoch,
		Round:   currentRound,
	}

	mt := tb.classifyMessage(m, epoch)
	switch mt {
	case TimelyMessage:
		for _, hash := range m.HashList() {
			tb.votesMu.Lock()
			tb.votes[pair] = append(tb.votes[pair], hash)
			tb.votesMu.Unlock()
		}

		return tb.beaconCalculator.CountBatchVotes(m) // TODO(nkryuchkov): count votes properly

	case DelayedMessage:
		tb.Log.Debug("Received delayed BatchVotingMessage, epoch: %v, proposals: %v, hash list: %v",
			m.Epoch(), m.Round(), m.HashList())
		return nil

	case LateMessage:
		tb.Log.Debug("Received late BatchVotingMessage, epoch: %v, round: %v, hash list: %v",
			m.Epoch(), m.Round(), m.HashList())
		return nil

	default:
		return ErrUnknownMessageType
	}
}

func (tb *TortoiseBeacon) classifyMessage(m Message, epoch types.EpochID) MessageType {
	tb.currentRoundsMu.Lock()
	currentRound := tb.currentRounds[epoch]
	tb.currentRoundsMu.Unlock()

	round := 0
	if m, ok := m.(VotingMessage); ok {
		round = m.Round()
	}

	switch {
	case round <= currentRound-1:
		return TimelyMessage
	case round == currentRound-2:
		return DelayedMessage
	default:
		return LateMessage
	}
}

// For K rounds: In each round that lasts δ, wait for proposals to come in.
func (tb *TortoiseBeacon) roundTicker(epoch types.EpochID) {
	ticker := time.NewTicker(tb.networkDelta)
	defer ticker.Stop()

	tb.currentRoundsMu.Lock()
	tb.currentRounds[epoch] = 0
	tb.currentRoundsMu.Unlock()

	// Round 0 is already happened at this point, starting from round 1.
	// For next rounds,
	// wait for δ time, and construct a message that points to all messages from previous round received by δ.
	for i := 1; i < tb.lastPossibleRound(); i++ {
		select {
		case <-ticker.C:
			tb.currentRoundsMu.Lock()
			tb.currentRounds[epoch]++
			round := tb.currentRounds[epoch]
			tb.currentRoundsMu.Unlock()

			if i == 1 {
				// round 1, send hashed proposal
				// create a voting message that references all seen proposals within δ time frame and send it
				tb.timelyProposalsMu.RLock()
				proposals := tb.timelyProposalsList[epoch]
				tb.timelyProposalsMu.RUnlock()

				hashes := make([]types.Hash32, 0, len(proposals))
				for _, p := range proposals {
					hashes = append(hashes, hashATXList(p))
				}

				m := NewBatchVotingMessage(epoch, round, hashes)
				tb.messageSender.Send(m)
			} else {
				// next rounds, send vote
				// construct a message that points to all messages from previous round received by δ
				key := EpochRoundPair{
					EpochID: epoch,
					Round:   round - 1, // proposals from the previous round are needed
				}

				tb.timelyProposalsMu.Lock()
				votes := tb.votes[key]
				tb.timelyProposalsMu.Unlock()

				m := NewBatchVotingMessage(epoch, round, votes)
				tb.messageSender.Send(m)
			}

		case <-tb.CloseChannel():
			return
		}
	}
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
func (tb *TortoiseBeacon) lastPossibleRound() int {
	return tb.config.K + 2
}

func hashATXList(atxList []types.ATXID) types.Hash32 {
	hash := sha256.New()

	for _, id := range atxList {
		if _, err := hash.Write(id.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var res types.Hash32
	hash.Sum(res[:0])

	return res
}
