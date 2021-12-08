package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func newCommonState() commonState {
	return commonState{
		blocks:           map[types.LayerID][]types.BlockID{},
		ballots:          map[types.LayerID][]types.BallotID{},
		ballotLayer:      map[types.BallotID]types.LayerID{},
		blockLayer:       map[types.BlockID]types.LayerID{},
		refBallotBeacons: map[types.EpochID]map[types.BallotID][]byte{},
		badBeaconBallots: map[types.BallotID]struct{}{},
		epochWeight:      map[types.EpochID]weight{},
		ballotWeight:     map[types.BallotID]weight{},
		localOpinion:     Opinion{},
	}
}

type commonState struct {
	// last received layer
	// TODO should be last layer according to the clock
	last types.LayerID
	// last verified layer
	verified types.LayerID
	// historicallyVerified matters only for local opinion for verifying tortoise
	// during rerun. for live tortoise it is identical to the verified layer.
	historicallyVerified types.LayerID
	// last processed layer
	processed types.LayerID
	// last layer with good ballots
	good types.LayerID
	// last evicted layer
	evicted types.LayerID

	blocks      map[types.LayerID][]types.BlockID
	ballots     map[types.LayerID][]types.BallotID
	ballotLayer map[types.BallotID]types.LayerID
	blockLayer  map[types.BlockID]types.LayerID

	// cache ref ballot by epoch
	refBallotBeacons map[types.EpochID]map[types.BallotID][]byte
	// cache ballots with bad beacons. this cache is mainly for self-healing where we only have BallotID
	// in the opinions map to work with.
	badBeaconBallots map[types.BallotID]struct{}

	// epochWeight average weight of atx's that target keyed epoch
	epochWeight map[types.EpochID]weight

	ballotWeight map[types.BallotID]weight

	localOpinion Opinion
}

func newFullState() fullState {
	return fullState{votes: map[types.BallotID]Opinion{}}
}

// fullState contains state necessary to run full (self-healing) tortoise.
//
// as long as the verifying tortoise makes progress this state can remain empty.
// if verifying tortoise get stuck we need to have data for two use cases:
// - select a base ballot that is the least bad, requires votes since genesis,
//   for now we keep votes only in a small window and only after verifying tortoise is stuck
//   relevant only if we can't find a good ballot that can fit all votes int the exception limit
// - verify layers with full vote counting, requires votes from ballots after verified layer
//
// TODO(dshulyak) this is a temporary data structure, until full tortoise will be refactored the same
// way as verifying tortoise.
type fullState struct {
	votes map[types.BallotID]Opinion
}
