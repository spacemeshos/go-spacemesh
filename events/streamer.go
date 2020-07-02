package events

import "github.com/spacemeshos/go-spacemesh/common/types"

// streamer is the event streamer singleton.
var streamer *EventStreamer

// Stream streams an event on the streamer singleton.
func StreamNewTx(tx *types.Transaction) {
	if streamer != nil {
		streamer.channelTransaction <- tx
	}
}

// Stream streams an event on the streamer singleton.
func StreamNewActivation(activation *types.ActivationTx) {
	if streamer != nil {
		streamer.channelActivation <- activation
	}
}

func GetNewTxStream() chan *types.Transaction {
	if streamer != nil {
		return streamer.channelTransaction
	}
	return nil
}

func GetActivationStream() chan *types.ActivationTx {
	if streamer != nil {
		return streamer.channelActivation
	}
	return nil
}

// InitializeEventStream initializes the event streaming interface
func InitializeEventStream() {
	streamer = NewEventStreamer()
}

// EventStreamer is the struct that streams events to API listeners
type EventStreamer struct {
	channelTransaction chan *types.Transaction
	channelActivation  chan *types.ActivationTx
}

func NewEventStreamer() *EventStreamer {
	return &EventStreamer{
		channelTransaction: make(chan *types.Transaction),
		channelActivation:  make(chan *types.ActivationTx),
	}
}

func CloseEventStream() {
	if streamer != nil {
		close(streamer.channelTransaction)
		close(streamer.channelActivation)
		streamer = nil
	}
}
