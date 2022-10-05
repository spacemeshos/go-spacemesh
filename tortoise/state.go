package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

type (
	weight = util.Weight

	verifyingInfo struct {
		// goodUncounted is a weight that doesn't vote for this layer
		//
		// for example ballot created in layer 10 doesn't vote for layers
		// 10 and above, therefore its weight needs to be added to goodUncounted
		// for layers 10 and above
		goodUncounted   weight
		abstained       weight
		referenceHeight uint64
	}

	epochInfo struct {
		atxs map[types.ATXID]uint64
		// weight is a sum of all atxs
		weight uint64
		// median height from atxs
		height uint64
	}

	layerInfo struct {
		lid            types.LayerID
		empty          weight
		hareTerminated bool
		blocks         []*blockInfo
		verifying      verifyingInfo
	}

	blockInfo struct {
		id     types.BlockID
		layer  types.LayerID
		height uint64
		hare   sign
		margin weight

		validity sign
		// if validity requires persisting it to the disk
		dirty bool
	}

	state struct {
		// last received layer
		// TODO should be last layer according to the clock
		last types.LayerID
		// localThreshold is updated together with the last layer.
		localThreshold util.Weight

		// last verified layer
		verified types.LayerID
		// last processed layer
		processed types.LayerID
		// last evicted layer
		evicted types.LayerID

		epochs  map[types.EpochID]*epochInfo
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
		epochs:     map[types.EpochID]*epochInfo{},
		layers:     map[types.LayerID]*layerInfo{},
		ballots:    map[types.LayerID][]*ballotInfo{},
		ballotRefs: map[types.BallotID]*ballotInfo{},
		blockRefs:  map[types.BlockID]*blockInfo{},
	}
}

func (s *state) globalThreshold(cfg Config, target types.LayerID) weight {
	return computeGlobalThreshold(cfg, s.localThreshold, s.epochs, target, s.processed, s.last)
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

func (s *state) addBlock(block *blockInfo) {
	layer := s.layer(block.layer)
	layer.blocks = append(layer.blocks, block)
	s.blockRefs[block.id] = block
}

func (s *state) updateRefHeight(layer *layerInfo, block *blockInfo) error {
	if layer.verifying.referenceHeight == 0 && layer.lid.After(s.evicted) {
		layer.verifying.referenceHeight = s.layer(layer.lid.Sub(1)).verifying.referenceHeight
	}
	epoch, exist := s.epochs[block.layer.GetEpoch()]
	if !exist {
		return fmt.Errorf("reference height for epoch %d is not recorded", block.layer.GetEpoch())
	}
	if block.height <= epoch.height &&
		block.height > layer.verifying.referenceHeight {
		layer.verifying.referenceHeight = block.height
	}
	return nil
}

type (
	baseInfo struct {
		id    types.BallotID
		layer types.LayerID
	}

	conditions struct {
		baseGood bool
		// is ballot consistent with local opinion within [base.layer, layer)
		consistent bool
		// set after comparing with local beacon
		badBeacon bool
		// any exceptions before base.layer
		votesBeforeBase bool
	}

	referenceInfo struct {
		weight weight
		height uint64
		beacon types.Beacon
	}

	ballotInfo struct {
		id         types.BallotID
		layer      types.LayerID
		base       baseInfo
		weight     weight
		reference  *referenceInfo
		votes      votes
		conditions conditions
	}
)

func (b *ballotInfo) good() bool {
	return b.canBeGood() && b.conditions.baseGood
}

func (b *ballotInfo) canBeGood() bool {
	return !b.conditions.badBeacon && !b.conditions.votesBeforeBase && b.conditions.consistent
}

type votes struct {
	tail *layerVote
}

func (v *votes) append(lv *layerVote) {
	if v.tail == nil {
		v.tail = lv
	} else {
		v.tail = v.tail.append(lv)
	}
}

func (v *votes) update(from types.LayerID, diff map[types.LayerID]map[types.BlockID]sign) votes {
	if v.tail == nil {
		return votes{}
	}
	return votes{tail: v.tail.update(from, diff)}
}

// cutBefore cuts all pointers to votes before the specified layer.
func (v *votes) cutBefore(lid types.LayerID) {
	for current := v.tail; current != nil; current = current.prev {
		prev := current.prev
		if prev != nil && prev.lid.Before(lid) {
			current.prev = nil
			return
		}
		if current.lid.Before(lid) {
			v.tail = nil
			return
		}
	}
}

func (v *votes) find(lid types.LayerID, bid types.BlockID) sign {
	for current := v.tail; current != nil; current = current.prev {
		if current.lid == lid {
			for _, block := range current.supported {
				if block.id == bid {
					return support
				}
			}
			return against
		}
	}
	return abstain
}

type layerVote struct {
	*layerInfo
	vote      sign
	supported []*blockInfo

	prev *layerVote
}

func (l *layerVote) getVote(block types.BlockID) sign {
	for _, info := range l.supported {
		if info.id == block {
			return support
		}
	}
	return against
}

func (l *layerVote) copy() *layerVote {
	return &layerVote{
		layerInfo: l.layerInfo,
		vote:      l.vote,
		supported: l.supported,
		prev:      l.prev,
	}
}

func (l *layerVote) append(lv *layerVote) *layerVote {
	lv.prev = l
	return lv
}

func (l *layerVote) update(from types.LayerID, diff map[types.LayerID]map[types.BlockID]sign) *layerVote {
	if l.lid.Before(from) {
		return l
	}
	l = l.copy()
	if l.prev != nil {
		l.prev = l.prev.update(from, diff)
	}
	layerdiff, exist := diff[l.lid]
	if exist && len(layerdiff) == 0 {
		l.vote = abstain
	} else if exist && len(layerdiff) > 0 {
		var supported []*blockInfo
		for _, block := range l.blocks {
			vote, exist := layerdiff[block.id]
			if exist && vote == support {
				supported = append(supported, block)
			}
			if !exist && l.getVote(block.id) == support {
				supported = append(supported, block)
			}
		}
		l.supported = supported
	}
	return l
}
