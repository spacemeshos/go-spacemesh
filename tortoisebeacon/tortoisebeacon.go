package tortoisebeacon

import (
	"errors"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	protoName      = "TORTOISE_BEACON_PROTOCOL"
	messageBufSize = 1024
)

var (
	ErrBadMessage  = errors.New("bad message")
	ErrUnknownType = errors.New("unknown type")
)

type messageReceiver interface {
	Receive() Message
}

type messageSender interface {
	Send(Message)
}

type TortoiseBeacon struct {
	hare.Closer
	log.Log

	config Config

	messageReceiver messageReceiver
	messageSender   messageSender

	atxDB *activation.DB

	layerMu   sync.RWMutex
	lastLayer types.LayerID

	layerTicker  chan types.LayerID
	networkDelta time.Duration

	initialMessagesMu sync.RWMutex
	initialMessages   map[types.EpochID]chan Message

	votingMessagesMu sync.RWMutex
	votingMessages   map[types.EpochID]chan Message

	currentRoundsMu sync.RWMutex
	currentRounds   map[types.EpochID]int

	timelyProposalsMu sync.RWMutex
	timelyProposals   map[EpochRoundPair][]types.ATXID

	delayedProposalsMu sync.RWMutex
	delayedProposals   map[EpochRoundPair][]types.ATXID

	lateProposalsMu sync.RWMutex
	lateProposals   map[EpochRoundPair][]types.ATXID
}

type EpochRoundPair struct {
	EpochID types.EpochID
	Round   int
}

func New(conf Config, messageReceiver messageReceiver, messageSender messageSender, atxDB *activation.DB, layerTicker chan types.LayerID, logger log.Log) *TortoiseBeacon {
	return &TortoiseBeacon{
		config:           conf,
		Log:              logger,
		messageReceiver:  messageReceiver,
		messageSender:    messageSender,
		atxDB:            atxDB,
		layerTicker:      layerTicker,
		networkDelta:     time.Duration(conf.WakeupDelta) * time.Second,
		initialMessages:  make(map[types.EpochID]chan Message),
		votingMessages:   make(map[types.EpochID]chan Message),
		currentRounds:    make(map[types.EpochID]int),
		timelyProposals:  make(map[EpochRoundPair][]types.ATXID),
		delayedProposals: make(map[EpochRoundPair][]types.ATXID),
		lateProposals:    make(map[EpochRoundPair][]types.ATXID),
	}
}

// Start starts listening for layers and outputs.
func (tb *TortoiseBeacon) Start() error {
	tb.Log.Info("Starting %v", protoName)

	go tb.tickLoop()
	go func() {
		if err := tb.listen(); err != nil {
			// TODO: handle error
			return
		}
	}()

	return nil
}

func (tb *TortoiseBeacon) listen() error {
	for {
		m := tb.messageReceiver.Receive()

		if err := tb.handleMessage(m); err != nil {
			return err
		}
	}
}

func (tb *TortoiseBeacon) handleMessage(m Message) error {
	switch m.Kind() {
	case InitialMessage:
		return tb.handleInitialMessage(m)
	case VotingMessage:
		return tb.handleVotingMessage(m)
	default:
		return ErrUnknownType
	}
}

// listens to new layers.
func (tb *TortoiseBeacon) tickLoop() {
	for {
		select {
		case layer := <-tb.layerTicker:
			go tb.onTick(layer)
		case <-tb.CloseChannel():
			return
		}
	}
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new CPs.
func (tb *TortoiseBeacon) onTick(layer types.LayerID) {
	tb.layerMu.Lock()
	if layer > tb.lastLayer {
		tb.lastLayer = layer
	}

	tb.layerMu.Unlock()

	epoch := layer.GetEpoch()
	tb.Debug("tortoise beacon got tick at layer %v epoch %v, sleeping for %v", layer, epoch, tb.networkDelta)

	if layer.GetEpoch().IsGenesis() {
		tb.With().Info("not starting tortoise beacon since we are in genesis epoch", layer)
		return
	}

	tb.initialMessagesMu.RLock()
	defer tb.initialMessagesMu.RUnlock() // TODO: check if unlocked at correct time

	if _, ok := tb.initialMessages[epoch]; ok {
		// Tortoise beacon already started for this epoch.
		return
	}

	tb.initialMessages[epoch] = make(chan Message, messageBufSize)

	go func() {
		tb.roundTicker(epoch)
	}()

	go func() {
		tb.listenInitialMessages(epoch)
	}()

	atxList := tb.atxDB.GetEpochAtxs(epoch - 1)
	m := NewVotingMessage(epoch, 0, atxList)
	tb.messageSender.Send(m)
}

func (tb *TortoiseBeacon) handleInitialMessage(m Message) error {
	epoch := m.Epoch()

	tb.initialMessagesMu.Lock()
	defer tb.initialMessagesMu.Unlock()

	if _, ok := tb.initialMessages[epoch]; !ok {
		return ErrBadMessage
	}

	tb.initialMessages[epoch] <- m

	return nil
}

func (tb *TortoiseBeacon) handleVotingMessage(m Message) error {
	epoch := m.Epoch()

	tb.votingMessagesMu.RLock()
	defer tb.votingMessagesMu.RUnlock()

	if _, ok := tb.votingMessages[epoch]; !ok {
		return ErrBadMessage
	}

	tb.votingMessages[epoch] <- m

	return nil
}

func (tb *TortoiseBeacon) listenInitialMessages(epoch types.EpochID) {
	tb.votingMessagesMu.Lock()
	ch := tb.votingMessages[epoch]
	tb.votingMessagesMu.Unlock()
	for m := range ch {
		tb.currentRoundsMu.Lock()
		currentRound := tb.currentRounds[epoch]
		tb.currentRoundsMu.Unlock()

		pair := EpochRoundPair{
			EpochID: epoch,
			Round:   currentRound,
		}

		if m.Round() <= currentRound {
			tb.timelyProposalsMu.Lock()
			tb.timelyProposals[pair] = m.Payload()
			tb.timelyProposalsMu.Unlock()
		} else if m.Round() == currentRound-1 {
			tb.delayedProposalsMu.Lock()
			tb.delayedProposals[pair] = m.Payload()
			tb.delayedProposalsMu.Unlock()
		} else {
			tb.lateProposalsMu.Lock()
			tb.lateProposals[pair] = m.Payload()
			tb.lateProposalsMu.Unlock()
		}
	}
}

func (tb *TortoiseBeacon) roundTicker(epoch types.EpochID) {
	ticker := time.NewTicker(tb.networkDelta)

	tb.currentRoundsMu.Lock()
	tb.currentRounds[epoch] = 0
	tb.currentRoundsMu.Unlock()

	for i := 1; i < tb.lastPossibleRound(); i++ {
		<-ticker.C
		tb.currentRoundsMu.Lock()
		tb.currentRounds[epoch]++
		round := tb.currentRounds[epoch]
		tb.currentRoundsMu.Unlock()

		pair := EpochRoundPair{
			EpochID: epoch,
			Round:   round,
		}

		tb.timelyProposalsMu.Lock()
		proposals := tb.timelyProposals[pair]
		tb.timelyProposalsMu.Unlock()

		m := NewVotingMessage(epoch, round, proposals)
		tb.messageSender.Send(m)
	}

	ticker.Stop()
}

func (tb *TortoiseBeacon) lastPossibleRound() int {
	// K - 1 is the last round (counting starts from 0).
	// That means, messages from round K - 1 received in round K - 1 are timely.
	// Messages from round K - 1 received in round K are delayed.
	// Messages from round K - 1 received in round K + 1 are late.
	// Therefore, counting more than K + 1 rounds is not needed.

	return tb.config.K + 1
}
