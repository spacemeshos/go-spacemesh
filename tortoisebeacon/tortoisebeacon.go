package tortoisebeacon

import (
	"errors"
	"sync"
	"time"

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

	layerMu   sync.RWMutex
	lastLayer types.LayerID

	layerTicker  chan types.LayerID
	networkDelta time.Duration

	initialMessagesMu sync.RWMutex
	initialMessages   map[types.EpochID]chan Message

	votingMessagesMu sync.RWMutex
	votingMessages   map[types.EpochID]chan Message
}

func New(conf Config, messageReceiver messageReceiver, messageSender messageSender, layerTicker chan types.LayerID, logger log.Log) *TortoiseBeacon {
	return &TortoiseBeacon{
		config:          conf,
		Log:             logger,
		messageReceiver: messageReceiver,
		messageSender:   messageSender,
		layerTicker:     layerTicker,
		networkDelta:    time.Duration(conf.WakeupDelta) * time.Second,
		initialMessages: make(map[types.EpochID]chan Message),
		votingMessages:  make(map[types.EpochID]chan Message),
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

	message := NewInitialMessage(epoch)
	tb.messageSender.Send(message)

	ti := time.NewTimer(tb.networkDelta)
	select {
	case <-ti.C:
		break // keep going
	case <-tb.CloseChannel():
		// closed while waiting the delta
		return
	}
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
