package tortoisebeacon

import (
	"errors"
	"sync"
	"time"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	protoName       = "TORTOISE_BEACON_PROTOCOL"
	cleanupInterval = 30 * time.Second
	cleanupEpochs   = 20
)

var ErrUnknownMessageType = errors.New("unknown message type")

type messageReceiver interface {
	Receive() Message
}

type messageSender interface {
	Send(Message) // TODO(nkryuchkov): sign message
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

	closed chan struct{} // closed if TortoiseBeacon is Close()'d.

	messageReceiver  messageReceiver
	messageSender    messageSender
	beaconCalculator beaconCalculator

	atxDB *activation.DB

	layerMu   sync.RWMutex
	lastLayer types.LayerID

	layerTicker  chan types.LayerID
	networkDelta time.Duration

	currentRoundsMu sync.RWMutex
	currentRounds   map[types.EpochID]int

	timelyProposalsMu   sync.RWMutex
	timelyProposalsMap  map[types.Hash32][]types.ATXID // lookup by hash
	timelyProposalsList map[types.EpochID][][]types.ATXID

	// TODO(nkryuchkov): consider removing
	delayedProposalsMu sync.RWMutex
	delayedProposals   map[types.Hash32][]types.ATXID

	// TODO(nkryuchkov): consider removing
	lateProposalsMu sync.RWMutex
	lateProposals   map[types.Hash32][]types.ATXID

	votesMu sync.RWMutex
	votes   map[EpochRoundPair][]types.Hash32

	beaconsMu sync.RWMutex
	beacons   map[types.EpochID]types.Hash32
	// beaconsCh's value has channel type to be able to implement getting a beacon value in a blocking way,
	// i.e. read from the channel
	beaconsCh map[types.EpochID]chan types.Hash32
}

func New(conf Config, messageReceiver messageReceiver, messageSender messageSender, beaconCalculator beaconCalculator, atxDB *activation.DB, layerTicker chan types.LayerID, logger log.Log) *TortoiseBeacon {
	return &TortoiseBeacon{
		config:             conf,
		Log:                logger,
		closed:             make(chan struct{}),
		messageReceiver:    messageReceiver,
		messageSender:      messageSender,
		beaconCalculator:   beaconCalculator,
		atxDB:              atxDB,
		layerTicker:        layerTicker,
		networkDelta:       time.Duration(conf.WakeupDelta) * time.Second,
		currentRounds:      make(map[types.EpochID]int),
		timelyProposalsMap: make(map[types.Hash32][]types.ATXID),
		delayedProposals:   make(map[types.Hash32][]types.ATXID),
		lateProposals:      make(map[types.Hash32][]types.ATXID),
	}
}

// Start starts listening for layers and outputs.
func (tb *TortoiseBeacon) Start() error {
	tb.Log.Info("Starting %v", protoName)

	go tb.listenLayers()

	go func() {
		if err := tb.listenMessages(); err != nil {
			// TODO(nkryuchkov): handle error
			return
		}
	}()

	go tb.cleanupLoop()

	return nil
}

func (tb *TortoiseBeacon) Close() error {
	tb.Log.Info("Closing %v", protoName)
	close(tb.closed)

	return nil
}

func (tb *TortoiseBeacon) Get(epochID types.EpochID) (types.Hash32, bool) {
	tb.beaconsMu.RLock()
	beacon, ok := tb.beacons[epochID]
	tb.beaconsMu.RUnlock()

	return beacon, ok
}

func (tb *TortoiseBeacon) WaitAndGet(epochID types.EpochID) types.Hash32 {
	tb.beaconsMu.RLock()
	ch := tb.beaconsCh[epochID]
	tb.beaconsMu.RUnlock()

	beacon := <-ch
	return beacon
}

func (tb *TortoiseBeacon) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tb.closed:
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
			delete(tb.beaconsCh, e)
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
		m := tb.messageReceiver.Receive()

		if err := tb.handleMessage(m); err != nil {
			return err
		}
	}
}

// listens to new layers.
func (tb *TortoiseBeacon) listenLayers() {
	for {
		select {
		case layer := <-tb.layerTicker:
			go tb.handleLayer(layer)
		case <-tb.CloseChannel():
			return
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

	tb.beaconsCh[epoch] = make(chan types.Hash32, 1)
	tb.beaconsMu.Unlock()

	// round 0
	// take all ATXs received in last epoch (i -1)
	atxList := tb.atxDB.GetEpochAtxs(epoch - 1)

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
	// store value in two ways for blocking and non-blocking retrieval
	tb.beacons[epoch] = beacon
	tb.beaconsCh[epoch] <- beacon
	close(tb.beaconsCh[epoch])
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
	default:
		// TODO(nkryuchkov): handle other types
		return nil
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

	default:
		// TODO(nkryuchkov): handle other types
		return nil
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

	default:
		// TODO(nkryuchkov): handle other types
		return nil
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

// for K rounds : in each round that lasts δ, wait for proposals to come in
func (tb *TortoiseBeacon) roundTicker(epoch types.EpochID) {
	ticker := time.NewTicker(tb.networkDelta)
	defer ticker.Stop()

	tb.currentRoundsMu.Lock()
	tb.currentRounds[epoch] = 0
	tb.currentRoundsMu.Unlock()

	// round 0 already happened at this point, starting from round 1
	// for next rounds wait for δ time, and construct a message that points to all messages from previous round received by δ
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

				for _, p := range proposals {
					hash := hashATXList(p)

					m := NewVotingMessage(epoch, round, hash)
					tb.messageSender.Send(m)
				}
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

				for _, voteHash := range votes {
					m := NewVotingMessage(epoch, round, voteHash)
					tb.messageSender.Send(m)
				}
			}

		case <-tb.CloseChannel():
			return
		}
	}
}

// Each smesher partitions the valid proposals received in the previous epoch into three sets:
// - Timely proposals: received up to δ after the end of the previous epoch.
// - Delayed proposals: received between δ and 2δ after the end of the previous epoch.
// - Late proposals: more than 2δ after the end of the previous epoch. Note that honest users cannot disagree on timing by more than δ, so if a proposal is timely for any honest user, it cannot be late for any honest user (and vice versa).
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
		hash.Write(id.Bytes()) // this never returns an error: https://golang.org/pkg/hash/#Hash
	}
	var res types.Hash32
	hash.Sum(res[:0])
	return res
}
