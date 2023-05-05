package model

import (
	"container/list"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Message is an alias to interface.
type Message any

type (
	// Layer clock events. Reordering them means that node has issue with local clock,
	// so for the purposes of the tests in this model they are never reordered.

	// MessageLayerStart ...
	MessageLayerStart struct {
		LayerID types.LayerID
	}

	// MessageLayerEnd ...
	MessageLayerEnd struct {
		LayerID types.LayerID
	}
)

type (
	// Node events. Messages that are produced by every node, triggered by layer clock events.

	// MessageAtx is an event producing an atx.
	MessageAtx struct {
		Atx *types.ActivationTx
	}

	// MessageBallot is an event for ballots.
	MessageBallot struct {
		Ballot *types.Ballot
	}
)

type (
	// Hare consensus events. Multiple hare instances implies that
	// there is a network split or a bug. For purposes of this model
	// first block that is received is considered as a certified hare output.

	// MessageBlock is an event producing a block.
	MessageBlock struct {
		Block *types.Block
	}

	// MessageCoinflip is an event producing coinflip.
	MessageCoinflip struct {
		LayerID  types.LayerID
		Coinflip bool
	}
)

// MessageBeacon is an event producing a beacon for epoch.
// There could be multiple instances of beacon or none.
// "Honest" core machine should produce random beacon if it didn't
// receive beacon from messages.
type MessageBeacon struct {
	EpochID types.EpochID
	Beacon  types.Beacon
}

// Messenger is an interface for communicating with state machines and monitors.
type Messenger interface {
	Send(...Message)
	Notify(...Event)
	PopMessage() Message
	PopEvent() Event
}

type reliableMessenger struct {
	messages list.List
	events   list.List
}

func (r *reliableMessenger) Send(msgs ...Message) {
	for _, msg := range msgs {
		r.messages.PushBack(msg)
	}
}

func (r *reliableMessenger) Notify(events ...Event) {
	for _, ev := range events {
		r.events.PushBack(ev)
	}
}

func (r *reliableMessenger) PopMessage() Message {
	if r.messages.Len() == 0 {
		return nil
	}
	return r.messages.Remove(r.messages.Front())
}

func (r *reliableMessenger) PopEvent() Event {
	if r.events.Len() == 0 {
		return nil
	}
	return r.events.Remove(r.events.Front())
}
