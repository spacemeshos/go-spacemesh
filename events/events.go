// Package events defines events published by go-spacemsh node using nodes pubsub
package events

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// These consts are used as prefixes for different messages in pubsub
const (
	EventNewBlock ChannelID = 1 + iota
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
		if err := publisher.PublishEvent(event); err != nil {
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
	GetChannel() ChannelID
}

// NewEventPublisher is a constructor for the event publisher, it received a url string in format of tcp://localhost:56565 to start
// listening for connections.
func NewEventPublisher(eventURL string) (*EventPublisher, error) {
	p, err := newPublisher(eventURL)
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

// Close closes the published internal socket
func (p *EventPublisher) Close() error {
	return p.sock.Close()
}

// NewBlock is sent when a new block is created by this miner
type NewBlock struct {
	ID    string
	Layer uint64
	Atx   string
}

// GetChannel gets the message type which means on which this message should be sent
func (NewBlock) GetChannel() ChannelID {
	return EventNewBlock
}

// DoneCreatingBlock signals that this miner has created a block
type DoneCreatingBlock struct {
	Eligible bool
	Layer    uint64
	Error    string
}

// GetChannel gets the message type which means on which this message should be sent
func (DoneCreatingBlock) GetChannel() ChannelID {
	return EventCreatedBlock
}

// ValidBlock signals that block with id ID has been validated
type ValidBlock struct {
	ID    string
	Valid bool
}

// GetChannel gets the message type which means on which this message should be sent
func (ValidBlock) GetChannel() ChannelID {
	return EventBlockValid
}

// NewAtx signals that a new ATX has been received
type NewAtx struct {
	ID      string
	LayerID uint64
}

// GetChannel gets the message type which means on which this message should be sent
func (NewAtx) GetChannel() ChannelID {
	return EventNewAtx
}

// ValidAtx signals that an activation transaction with id ID has been validated
type ValidAtx struct {
	ID    string
	Valid bool
}

// GetChannel gets the message type which means on which this message should be sent
func (ValidAtx) GetChannel() ChannelID {
	return EventAtxValid
}

// NewTx signals that a new transaction has been received and not yet validated
type NewTx struct {
	ID          string
	Origin      string
	Destination string
	Amount      uint64
	Fee         uint64
}

// GetChannel gets the message type which means on which this message should be sent
func (NewTx) GetChannel() ChannelID {
	return EventNewTx
}

// ValidTx signals that the transaction with id ID has been validated
type ValidTx struct {
	ID    string
	Valid bool
}

// GetChannel gets the message type which means on which this message should be sent
func (ValidTx) GetChannel() ChannelID {
	return EventTxValid
}

// RewardReceived signals reward has been received
type RewardReceived struct {
	Coinbase string
	Amount   uint64
}

// GetChannel gets the message type which means on which this message should be sent
func (RewardReceived) GetChannel() ChannelID {
	return EventRewardReceived
}

// AtxCreated signals this miner has created an activation transaction
type AtxCreated struct {
	Created bool
	ID      string
	Layer   uint64
}

// GetChannel gets the message type which means on which this message should be sent
func (AtxCreated) GetChannel() ChannelID {
	return EventCreatedAtx
}
