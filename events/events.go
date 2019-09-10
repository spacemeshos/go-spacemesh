package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	EventNewBlock channelId = 1 + iota
	EventBlockValid
	EventNewAtx
	EventAtxValid
	EventNewTx
	EventTxValid
	EventRewardReceived
)

// publisher is the event publisher singleton.
var publisher *EventPublisher

// Publish publishes an event on the pubsub singleton.
func Publish(event Event) {
	if publisher != nil {
		err := publisher.PublishEvent(event)
		if err != nil {
			log.Error("pubsub error: %v", err)
		}

	}
}

// InitializeEventPubsub initializes the global pubsub broadcaster server
func InitializeEventPubsub(ur string) {
	var err error
	publisher, err = NewEventPublisher(ur)
	if err != nil {
		log.Panic("cannot init pubsub: %v", err)
	} else {
		log.Info("pubsub created")
	}
}

// EventPublisher is the struct that publishes events to subscribers by topics.
type EventPublisher struct {
	*Publisher
}

// Event defines the interface that each message sent by the EventPublisher needs to implemet for it to correctly
// be routed by topic.
type Event interface {
	// getChannel returns the channel on which this message will be published.
	getChannel() channelId
}

// NewEventPublisher is a constructor for the event publisher, it received a url string in format of tcp://localhost:56565 to start
// listening for connections.
func NewEventPublisher(eventUrl string) (*EventPublisher, error) {
	p, err := newPublisher(eventUrl)
	if err != nil {
		return nil, err
	}
	return &EventPublisher{p}, nil
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

type NewBlock struct {
	Id    uint64
	Layer uint64
	Atx   string
}

func (NewBlock) getChannel() channelId {
	return EventNewBlock
}

type ValidBlock struct {
	Id    uint64
	Valid bool
}

func (ValidBlock) getChannel() channelId {
	return EventBlockValid
}

type NewAtx struct {
	Id string
}

func (NewAtx) getChannel() channelId {
	return EventNewAtx
}

type ValidAtx struct {
	Id    string
	Valid bool
}

func (ValidAtx) getChannel() channelId {
	return EventAtxValid
}

type NewTx struct {
	Id          string
	Origin      string
	Destination string
	Amount      uint64
	Gas         uint64
}

func (NewTx) getChannel() channelId {
	return EventNewTx
}

type ValidTx struct {
	Id    string
	Valid bool
}

func (ValidTx) getChannel() channelId {
	return EventTxValid
}

type RewardReceived struct {
	Coinbase string
	Amount   uint64
}

func (RewardReceived) getChannel() channelId {
	return EventRewardReceived
}
