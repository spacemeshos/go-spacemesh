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
		blocks []*blockInfo

		verifying verifyingInfo
	}

	blockInfo struct {
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
		ballots map[types.LayerID][]*ballotInfo

		// to efficiently find base and reference ballots
		ballotRefs map[types.BallotID]*ballotInfo
		// to efficiently decode exceptions
		blockRefs map[types.BlockID]*blockInfo
	}
)

func newState() *state {
	return &state{
		epochWeight:      map[types.EpochID]util.Weight{},
		referenceHeight:  map[types.EpochID]uint64{},
		badBeaconBallots: map[types.BallotID]struct{}{},
		referenceWeight:  map[types.BallotID]util.Weight{},

		layers:     map[types.LayerID]*layerInfo{},
		ballots:    map[types.LayerID][]*ballotInfo{},
		ballotRefs: map[types.BallotID]*ballotInfo{},
		blockRefs:  map[types.BlockID]*blockInfo{},
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

func (s *state) addBallot(ballot *ballotInfo) {
	s.ballots[ballot.layer] = append(s.ballots[ballot.layer], ballot)
	s.ballotRefs[ballot.id] = ballot
}

func (s *state) updateRefHeight(layer *layerInfo, block *blockInfo) {
	if layer.verifying.referenceHeight == 0 && layer.lid.After(s.evicted) {
		layer.verifying.referenceHeight = s.layer(layer.lid.Sub(1)).verifying.referenceHeight
	}
	if block.height <= s.referenceHeight[block.layer.GetEpoch()] &&
		block.height > layer.verifying.referenceHeight {
		layer.verifying.referenceHeight = block.height
	}
}

type (
	blockVote struct {
		*blockInfo
		vote vote
	}

	baseInfo struct {
		id    types.BallotID
		layer types.LayerID
	}

	ballotInfo struct {
		id     types.BallotID
		base   baseInfo
		layer  types.LayerID
		height uint64
		weight weight
		beacon types.Beacon

		votes Votes

		goodness condition
	}
)

func (v *ballotInfo) updateBlockVote(block *blockInfo, vote sign) {
	for current := v.votes.Tail; current != nil; current = current.prev {
		if current.lid == block.layer {
			for j := range current.blocks {
				if current.blocks[j].id == block.id {
					current.blocks[j].vote = vote
					return
				}
			}
		}
	}
}

func (v *ballotInfo) updateLayerVote(lid types.LayerID, vote sign) {
	for current := v.votes.Tail; current != nil; current = current.prev {
		if current.lid == lid {
			current.vote = vote
		}
	}
}

type Votes struct {
	Tail *layerVote
}

func (v *Votes) Append(lv *layerVote) {
	if v.Tail == nil {
		v.Tail = lv
	} else {
		v.Tail = v.Tail.Append(lv)
	}
}

func (v *Votes) Copy() Votes {
	if v.Tail == nil {
		return Votes{}
	}
	return Votes{Tail: v.Tail.Copy()}
}

// CutAt is used to cut pointer to the evicted layer.
func (v *Votes) CutAt(lid types.LayerID) {
	before := lid.Add(1)
	for current := v.Tail; current != nil; current = current.prev {
		if current.lid == before {
			current.prev = nil
			return
		}
	}
}

type layerVote struct {
	*layerInfo
	vote   vote
	blocks []blockVote

	prev *layerVote
}

func (l *layerVote) Copy() *layerVote {
	return &layerVote{
		layerInfo: l.layerInfo,
		vote:      l.vote,
		blocks:    l.blocks,
		prev:      l.prev,
	}
}

func (l *layerVote) Append(lv *layerVote) *layerVote {
	lv.prev = l
	return lv
}
