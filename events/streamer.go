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

func GetNewTxStream() chan *types.Transaction {
	if streamer != nil {
		return streamer.channelTransaction
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
}

func NewEventStreamer() *EventStreamer {
	return &EventStreamer{channelTransaction: make(chan *types.Transaction)}
}

func CloseEventStream() {
	if streamer != nil {
		close(streamer.channelTransaction)
		streamer = nil
	}
}
