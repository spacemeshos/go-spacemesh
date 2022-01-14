package model

import "github.com/spacemeshos/go-spacemesh/common/types"

// Event is an alias to interface.
type Event interface{}

type (
	// Layer clock events. Reordering them means that node has issue with local clock,
	// so for the purposes of the tests in this model they are never reordered.

	// EventLayerStart ...
	EventLayerStart struct {
		LayerID types.LayerID
	}

	// EventLayerEnd ...
	EventLayerEnd struct {
		LayerID types.LayerID
	}
)

type (
	// Node events. Events that are produced by every node, triggered by layer clock events.

	// EventAtx is an event producing an atx.
	EventAtx struct {
		Atx *types.ActivationTx
	}

	// EventBallot is an event for ballots.
	EventBallot struct {
		Ballot *types.Ballot
	}
)

type (
	// Hare consensus events. Multiple hare instances implies that
	// there is a network split or a bug. For purposes of this model
	// first block that is received is considered as a certified hare output.

	// EventBlock is an event producing a block.
	EventBlock struct {
		Block *types.Block
	}

	// EventCoinflip is an event producing coinflip.
	EventCoinflip struct {
		LayerID  types.LayerID
		Coinflip bool
	}
)

// EventBeacon is an event producing a beacon for epoch.
// There could be multiple instances of beacon or none.
// "Honest" core machine should produce random beacon if it didn't
// receive beacon from events.
type EventBeacon struct {
	EpochID types.EpochID
	Beacon  types.Beacon
}
