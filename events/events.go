package events

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
)

const (
	NewBlock channelId = 1 + iota
	BlockValid
	BlockInvalid
	NewAtx
	AtxValid
	AtxInvalid
	NewTx
	TxValid
	TxInvalid
	RewardReceived
)

// publisher is the event publisher singleton.
var publisher *EventPublisher

// Publish publishes an event on the pubsub singleton.
func Publish(event Event) {
	if publisher != nil {
		err := publisher.PublishEvent(event)
		log.Error("pubsub error: %v", err)
	}
}

// InitializeEventPubsub initializes the global pubsub broadcaster server
func InitializeEventPubsub(ur string) {
	var err error
	publisher, err = newEventPublisher(ur)
	if err != nil {
		log.Panic("cannot init pubsub: %v", err)
	}
}

// EventPublisher is the struct that publishes events to subscribers by topics.
type EventPublisher struct {
	Publisher
}

// Event defines the interface that each message sent by the EventPublisher needs to implemet for it to correctly
// be routed by topic.
type Event interface {
	// getChannel returns the channel on which this message will be published.
	getChannel() channelId
}

// newEventPublisher is a constructor for the event publisher, it received a url string in format of tcp://localhost:56565 to start
// listening for connections.
func newEventPublisher(eventUrl string) (*EventPublisher, error) {
	p, err := newPublisher(eventUrl)
	if err != nil {
		return nil, err
	}
	return &EventPublisher{*p}, nil
}

// PublishEvent publishes the provided event on pubsub infra. It encodes messages using XDR protocol.
func (p *EventPublisher) PublishEvent(event Event) error {
	bytes, err := types.InterfaceToBytes(event)
	if err != nil {
		return err
	}
	return p.publish(event.getChannel(), bytes)
}

func (p *EventPublisher) Close() error {
	return p.sock.Close()
}

type BasicEvent struct {
	Id byte
}

type NewBlockEvent struct {
	BasicEvent
	Layer uint64
	Block uint64
	Atx   string
}

func (NewBlockEvent) getChannel() channelId {
	return NewBlock
}

type BlockValidEvent struct {
	Block uint64
	Valid bool
}

func (BlockValidEvent) getChannel() channelId {
	return BlockValid
}

type NewAtxEvent struct {
	AtxId string
}

func (NewAtxEvent) getChannel() channelId {
	return NewAtx
}

type ValidAtxEvent struct {
	AtxId string
	Valid bool
}

func (ValidAtxEvent) getChannel() channelId {
	return AtxValid
}

type NewTxEvent struct {
	TxId        string
	Origin      string
	Destination string
	Amount      uint64
	Gas         uint64
}

func (NewTxEvent) getChannel() channelId {
	return NewTx
}

type ValidTxEvent struct {
	TxId  string
	Valid bool
}

func (ValidTxEvent) getChannel() channelId {
	return TxValid
}

type RewardReceivedEvent struct {
	Coinbase string
	Amount   uint64
}

func (RewardReceivedEvent) getChannel() channelId {
	return RewardReceived
}
