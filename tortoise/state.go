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
		refBallotBeacons: map[types.EpochID]map[types.BallotID]types.Beacon{},
		badBeaconBallots: map[types.BallotID]struct{}{},
		epochWeight:      map[types.EpochID]weight{},
		ballotWeight:     map[types.BallotID]weight{},
		undecided:        map[types.LayerID]struct{}{},
		hareOutput:       votes{},
		validity:         votes{},
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
	// last evicted layer
	evicted types.LayerID

	// localThreshold is changed when the last layer is updated.
	// computed as config.LocalThreshold * epochWeights[last].
	//
	// used as a part of threshold and for voting when block is undecided and outside of hdist.
	localThreshold weight
	// globalThreshold is changed when the last or verified layer is updated.
	// computed as - sum of all layers between verified + 1 up to last * config.GlobalThreshold.
	globalThreshold weight
	// threshold is a sum of local and global thresholds.
	// this is what we are using for deciding a block validity.
	threshold weight

	blocks      map[types.LayerID][]types.BlockID
	ballots     map[types.LayerID][]types.BallotID
	ballotLayer map[types.BallotID]types.LayerID
	blockLayer  map[types.BlockID]types.LayerID

	// cache ref ballot by epoch
	refBallotBeacons map[types.EpochID]map[types.BallotID]types.Beacon
	// cache ballots with bad beacons. this cache is mainly for self-healing where we only have BallotID
	// in the opinions map to work with.
	badBeaconBallots map[types.BallotID]struct{}

	// epochWeight average weight per layer of atx's that target keyed epoch
	epochWeight map[types.EpochID]weight

	ballotWeight map[types.BallotID]weight

	undecided  map[types.LayerID]struct{}
	hareOutput votes
	validity   votes
}
