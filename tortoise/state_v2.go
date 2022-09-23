package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

type (
	weight = util.Weight
	vote   = sign

	verifyingInfo struct {
		good, abstained weight
		referenceHeight uint64
	}

	layerInfo struct {
		lid            types.LayerID
		hareTerminated bool

		empty  weight
		blocks []*blockInfoV2

		verifying verifyingInfo
	}

	blockInfoV2 struct {
		id     types.BlockID
		layer  types.LayerID
		height uint64

		hare     vote
		validity vote
		margin   weight
	}

	state struct {
		// last received layer
		// TODO should be last layer according to the clock
		last types.LayerID
		// last verified layer
		verified types.LayerID
		// historicallyVerified matters only for local opinion for verifying tortoise
		// during rerun. for live tortoise it is identical to the verified layer.
		historicallyVerified types.LayerID
		// last processed layer
		minprocessed, processed types.LayerID
		// last evicted layer
		evicted types.LayerID

		localThreshold  weight
		globalThreshold weight

		// epochWeight average weight per layer of atx's that target keyed epoch
		epochWeight map[types.EpochID]weight
		// referenceHeight is a median height from all atxs that target keyed epoch
		referenceHeight map[types.EpochID]uint64
		// referenceWeight stores atx weight divided by the total number of eligibilities.
		// it is computed together with refBallot weight. it is not equal to refBallot
		// only if refBallot has more than 1 eligibility proof.
		referenceWeight  map[types.BallotID]weight
		badBeaconBallots map[types.BallotID]struct{}

		layers  map[types.LayerID]*layerInfo
		ballots map[types.LayerID][]ballotInfoV2

		// to efficiently find base and reference ballots
		ballotRefs map[types.BallotID]*ballotInfoV2
		// to efficiently decode exceptions
		blockRefs map[types.BlockID]*blockInfoV2
	}
)

func newState() *state {
	return &state{
		epochWeight:      map[types.EpochID]util.Weight{},
		referenceHeight:  map[types.EpochID]uint64{},
		badBeaconBallots: map[types.BallotID]struct{}{},
		referenceWeight:  map[types.BallotID]util.Weight{},

		layers:     map[types.LayerID]*layerInfo{},
		ballots:    map[types.LayerID][]ballotInfoV2{},
		ballotRefs: map[types.BallotID]*ballotInfoV2{},
		blockRefs:  map[types.BlockID]*blockInfoV2{},
	}
}

func (s *state) layer(lid types.LayerID) *layerInfo {
	layer, exist := s.layers[lid]
	if !exist {
		layer = &layerInfo{lid: lid, empty: util.WeightFromUint64(0)}
		s.layers[lid] = layer
	}
	return layer
}

func (s *state) addBallot(ballot ballotInfoV2) {
	s.ballots[ballot.layer] = append(s.ballots[ballot.layer], ballot)
	s.ballotRefs[ballot.id] = &ballot
}

type (
	layerVote struct {
		layer  *layerInfo
		vote   vote
		blocks []blockVote
	}

	blockVote struct {
		block *blockInfoV2
		vote  vote
	}

	baseInfo struct {
		id       types.BallotID
		layer    types.LayerID
		goodness goodness
	}

	ballotInfoV2 struct {
		id     types.BallotID
		base   baseInfo
		layer  types.LayerID
		height uint64
		weight weight
		beacon types.Beacon

		votes []layerVote

		goodness goodness
	}
)

func (b *ballotInfoV2) copyVotes(evicted types.LayerID) []layerVote {
	eid := 0
	for i := range b.votes {
		if !evicted.Before(b.votes[i].layer.lid) {
			eid = i
			break
		}
	}
	rst := make([]layerVote, len(b.votes)-eid)
	copy(rst, b.votes[eid:])
	return rst
}

func (v *ballotInfoV2) updateBlockVote(block *blockInfoV2, vote sign) {
	for i := len(v.votes) - 1; i >= 0; i-- {
		if v.votes[i].layer.lid == block.layer {
			for j := range v.votes[i].blocks {
				if v.votes[i].blocks[j].block.id == block.id {
					v.votes[i].blocks[j].vote = vote
					return
				}
			}
			return
		}
	}
}

func (v *ballotInfoV2) updateLayerVote(lid types.LayerID, vote sign) {
	for i := len(v.votes) - 1; i >= 0; i-- {
		if v.votes[i].layer.lid == lid {
			v.votes[i].vote = abstain
			break
		}
	}
}
