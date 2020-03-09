package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	EventNewBlock ChannelId = 1 + iota
	EventBlockValid
	EventNewAtx
	EventAtxValid
	EventNewTx
	EventTxValid
	EventRewardReceived
	EventCreatedBlock
	EventCreatedAtx
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
	// GetChannel returns the channel on which this message will be published.
	GetChannel() ChannelId
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
	return p.publish(event.GetChannel(), bytes)
}

func (p *EventPublisher) Close() error {
	return p.sock.Close()
}

type NewBlock struct {
	Id    string
	Layer uint64
	Atx   string
}

func (NewBlock) GetChannel() ChannelId {
	return EventNewBlock
}

type DoneCreatingBlock struct {
	Eligible bool
	Layer    uint64
	Error    string
}

func (DoneCreatingBlock) GetChannel() ChannelId {
	return EventCreatedBlock
}

type ValidBlock struct {
	Id    string
	Valid bool
}

func (ValidBlock) GetChannel() ChannelId {
	return EventBlockValid
}

type NewAtx struct {
	Id      string
	LayerId uint64
}

func (NewAtx) GetChannel() ChannelId {
	return EventNewAtx
}

type ValidAtx struct {
	Id    string
	Valid bool
}

func (ValidAtx) GetChannel() ChannelId {
	return EventAtxValid
}

type NewTx struct {
	Id          string
	Origin      string
	Destination string
	Amount      uint64
	Fee         uint64
}

func (NewTx) GetChannel() ChannelId {
	return EventNewTx
}

type ValidTx struct {
	Id    string
	Valid bool
}

func (ValidTx) GetChannel() ChannelId {
	return EventTxValid
}

type RewardReceived struct {
	Coinbase string
	Amount   uint64
}

func (RewardReceived) GetChannel() ChannelId {
	return EventRewardReceived
}

type AtxCreated struct {
	Created bool
	Id      string
	Layer   uint64
}

func (AtxCreated) GetChannel() ChannelId {
	return EventCreatedAtx
}
