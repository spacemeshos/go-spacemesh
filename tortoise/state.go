package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

type (
	weight = util.Weight

	verifyingInfo struct {
		good, abstained weight
		referenceHeight uint64
	}

	layerInfo struct {
		lid            types.LayerID
		empty          weight
		hareTerminated bool
		blocks         []*blockInfo
		verifying      verifyingInfo
	}

	blockInfo struct {
		id       types.BlockID
		layer    types.LayerID
		height   uint64
		hare     sign
		validity sign
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
		processed types.LayerID
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
		referenceWeight map[types.BallotID]weight

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
		epochWeight:     map[types.EpochID]util.Weight{},
		referenceHeight: map[types.EpochID]uint64{},
		referenceWeight: map[types.BallotID]util.Weight{},

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

func (s *state) addBlock(block *blockInfo) {
	layer := s.layer(block.layer)
	layer.blocks = append(layer.blocks, block)
	s.blockRefs[block.id] = block
}

func (s *state) updateRefHeight(layer *layerInfo, block *blockInfo) error {
	if layer.verifying.referenceHeight == 0 && layer.lid.After(s.evicted) {
		layer.verifying.referenceHeight = s.layer(layer.lid.Sub(1)).verifying.referenceHeight
	}
	refheight, exist := s.referenceHeight[block.layer.GetEpoch()]
	if !exist {
		return fmt.Errorf("reference height for epoch %d is not recorded", block.layer.GetEpoch())
	}
	if block.height <= refheight &&
		block.height > layer.verifying.referenceHeight {
		layer.verifying.referenceHeight = block.height
	}
	return nil
}

type (
	blockVote struct {
		*blockInfo
		vote sign
	}

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

	ballotInfo struct {
		id         types.BallotID
		layer      types.LayerID
		base       baseInfo
		height     uint64
		weight     weight
		beacon     types.Beacon
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
			for _, block := range current.blocks {
				if block.id == bid {
					return block.vote
				}
			}
		}
	}
	return abstain
}

type layerVote struct {
	*layerInfo
	vote   sign
	blocks []blockVote

	prev *layerVote
}

func (l *layerVote) copy() *layerVote {
	return &layerVote{
		layerInfo: l.layerInfo,
		vote:      l.vote,
		blocks:    l.blocks,
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
		blocks := make([]blockVote, len(l.blocks))
		copy(blocks, l.blocks)
		l.blocks = blocks
		for i := range l.blocks {
			vote, exist := layerdiff[l.blocks[i].id]
			if exist {
				l.blocks[i].vote = vote
			}
		}
	}
	return l
}
