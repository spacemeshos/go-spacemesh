package tortoise

import (
	"context"
	"math/big"

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
	}
}

type commonState struct {
	// last received layer
	// TODO should be last layer according to the clock
	last types.LayerID
	// last verified layer
	verified types.LayerID
	// last processed layer
	processed types.LayerID
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
}

func newFullState() fullState {
	return fullState{votes: map[types.BallotID]Opinion{}}
}

// fullState contains state necessary to run full (self-healing) tortoise.
//
// as long as the verifying tortoise makes progress this state can remain empty.
// if verifying tortoise get stuck we need to have data from verified layer up to last layer.
//
// TODO(dshulyak) this is a temporary data structure, until full tortoise will be refactored the same
// way as verifying tortoise.
type fullState struct {
	votes map[types.BallotID]Opinion
}

type state struct {
	// cache ref ballot by epoch
	refBallotBeacons map[types.EpochID]map[types.BallotID][]byte
	// cache ballots with bad beacons. this cache is mainly for self-healing where we only have BallotID
	// in the opinions map to work with.
	badBeaconBallots map[types.BallotID]struct{}

	// epochWeight average weight of atx's that target keyed epoch
	epochWeight map[types.EpochID]uint64

	// last layer processed: note that tortoise does not have a concept of "current" layer (and it's not aware of the
	// current time or latest tick). As far as Tortoise is concerned, Last is the current layer. This is a subjective
	// view of time, but Tortoise receives layers as soon as Hare finishes processing them or when they are received via
	// gossip, and there's nothing for Tortoise to verify without new data anyway.
	Last        types.LayerID
	Processed   types.LayerID
	LastEvicted types.LayerID
	Verified    types.LayerID
	// if key exists in the map the ballot is good. if value is true it was written to the disk.
	GoodBallotsIndex map[types.BallotID]bool
	// use 2D array to be able to iterate from the latest elements easily
	BallotOpinionsByLayer map[types.LayerID]map[types.BallotID]Opinion
	// BallotWeight is a weight of a single ballot.
	BallotWeight map[types.BallotID]*big.Float
	// BallotLayer stores reverse mapping from BallotOpinionsByLayer
	BallotLayer map[types.BallotID]types.LayerID
	// BlockLayer stores mapping from BlockID to LayerID
	BlockLayer map[types.BlockID]types.LayerID
}

func (s *state) Evict(ctx context.Context, windowStart types.LayerID) {
	oldestEpoch := windowStart.GetEpoch()

	for layerToEvict := s.LastEvicted.Add(1); layerToEvict.Before(windowStart); layerToEvict = layerToEvict.Add(1) {
		for ballot := range s.BallotOpinionsByLayer[layerToEvict] {
			delete(s.GoodBallotsIndex, ballot)
			delete(s.BallotLayer, ballot)
			delete(s.BallotWeight, ballot)
			delete(s.BlockLayer, types.BlockID(ballot))
			delete(s.badBeaconBallots, ballot)
		}
		delete(s.BallotOpinionsByLayer, layerToEvict)
		if layerToEvict.GetEpoch() < oldestEpoch {
			epoch := layerToEvict.GetEpoch()
			delete(s.refBallotBeacons, epoch)
			delete(s.epochWeight, epoch)
		}
	}
	s.LastEvicted = windowStart.Sub(1)
	return
}
