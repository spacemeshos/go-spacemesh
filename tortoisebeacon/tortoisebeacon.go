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
	ErrBadMessage = errors.New("bad message")
)

type messageReceiver interface {
	Receive() Message
}

type messageSender interface {
	Send(Message) // TODO: sign message
}

type beaconCalculator interface {
	CalculateBeacon(map[EpochRoundPair][]types.ATXID) types.Hash32
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

	atxDB *activation.DB

	layerMu   sync.RWMutex
	lastLayer types.LayerID

	layerTicker  chan types.LayerID
	networkDelta time.Duration

	currentRoundsMu sync.RWMutex
	currentRounds   map[types.EpochID]int

	timelyProposalsMu sync.RWMutex
	timelyProposals   map[EpochRoundPair][]types.ATXID

	delayedProposalsMu sync.RWMutex
	delayedProposals   map[EpochRoundPair][]types.ATXID

	lateProposalsMu sync.RWMutex
	lateProposals   map[EpochRoundPair][]types.ATXID

	beaconsMu sync.RWMutex
	beacons   map[types.EpochID]chan types.Hash32
}

func New(conf Config, messageReceiver messageReceiver, messageSender messageSender, beaconCalculator beaconCalculator, atxDB *activation.DB, layerTicker chan types.LayerID, logger log.Log) *TortoiseBeacon {
	return &TortoiseBeacon{
		config:           conf,
		Log:              logger,
		messageReceiver:  messageReceiver,
		messageSender:    messageSender,
		beaconCalculator: beaconCalculator,
		atxDB:            atxDB,
		layerTicker:      layerTicker,
		networkDelta:     time.Duration(conf.WakeupDelta) * time.Second,
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

func (tb *TortoiseBeacon) Get(epochID types.EpochID) types.Hash32 {
	tb.beaconsMu.RLock()
	ch := tb.beacons[epochID]
	tb.beaconsMu.RUnlock()

	beacon := <-ch
	return beacon
}

func (tb *TortoiseBeacon) listen() error {
	for {
		m := tb.messageReceiver.Receive()

		if err := tb.handleMessage(m); err != nil {
			return err
		}
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

	tb.beaconsMu.Lock()
	tb.beacons[epoch] = make(chan types.Hash32, 1)
	tb.beaconsMu.Unlock()

	go func() {
		tb.roundTicker(epoch)

		tb.timelyProposalsMu.Lock()
		beacon := tb.beaconCalculator.CalculateBeacon(tb.timelyProposals)
		tb.timelyProposalsMu.Unlock()

		tb.beaconsMu.Lock()
		tb.beacons[epoch] <- beacon
		close(tb.beacons[epoch])
		tb.beaconsMu.Unlock()
	}()

	atxList := tb.atxDB.GetEpochAtxs(epoch - 1)
	m := NewMessage(epoch, 0, atxList)
	tb.messageSender.Send(m)
}

func (tb *TortoiseBeacon) handleMessage(m Message) error {
	epoch := m.Epoch()
	tb.classifyMessage(m, epoch)

	return nil
}

func (tb *TortoiseBeacon) classifyMessage(m Message, epoch types.EpochID) {
	tb.currentRoundsMu.Lock()
	currentRound := tb.currentRounds[epoch]
	tb.currentRoundsMu.Unlock()

	pair := EpochRoundPair{
		EpochID: epoch,
		Round:   currentRound,
	}

	if m.Round() <= currentRound-1 {
		tb.timelyProposalsMu.Lock()
		tb.timelyProposals[pair] = m.Payload()
		tb.timelyProposalsMu.Unlock()
	} else if m.Round() == currentRound-2 {
		tb.delayedProposalsMu.Lock()
		tb.delayedProposals[pair] = m.Payload()
		tb.delayedProposalsMu.Unlock()
	} else {
		tb.lateProposalsMu.Lock()
		tb.lateProposals[pair] = m.Payload()
		tb.lateProposalsMu.Unlock()
	}
}

func (tb *TortoiseBeacon) roundTicker(epoch types.EpochID) {
	ticker := time.NewTicker(tb.networkDelta)
	defer ticker.Stop()

	tb.currentRoundsMu.Lock()
	tb.currentRounds[epoch] = 0
	tb.currentRoundsMu.Unlock()

	for i := 1; i < tb.lastPossibleRound(); i++ {
		select {
		case <-ticker.C:
			tb.currentRoundsMu.Lock()
			tb.currentRounds[epoch]++
			round := tb.currentRounds[epoch]
			tb.currentRoundsMu.Unlock()

			pair := EpochRoundPair{
				EpochID: epoch,
				Round:   round - 1, // proposals from the previous round are needed
			}

			tb.timelyProposalsMu.Lock()
			proposals := tb.timelyProposals[pair]
			tb.timelyProposalsMu.Unlock()

			m := NewMessage(epoch, round, proposals)
			tb.messageSender.Send(m)
		case <-tb.CloseChannel():
			return
		}
	}
}

func (tb *TortoiseBeacon) lastPossibleRound() int {
	// K - 1 is the last round (counting starts from 0).
	// That means:
	// Messages from round K - 1 received in round K are timely.
	// Messages from round K - 1 received in round K + 1 are delayed.
	// Messages from round K - 1 received in round K + 2 are late.
	// Therefore, counting more than K + 2 rounds is not needed.

	return tb.config.K + 2
}
