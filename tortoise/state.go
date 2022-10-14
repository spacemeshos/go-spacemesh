package tortoise

import (
	"fmt"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
)

var abstainSentinel = []byte{0}

type (
	weight = util.Weight

	verifyingInfo struct {
		good, abstained weight
		// highest block height below reference height
		referenceHeight uint64
	}

	layerInfo struct {
		lid            types.LayerID
		empty          weight
		hareTerminated bool
		blocks         []*blockInfo
		ballots        []*ballotInfo
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
		// https://github.com/spacemeshos/go-spacemesh/issues/2921
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

		layers map[types.LayerID]*layerInfo

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
	layer := s.layer(ballot.layer)
	layer.ballots = append(layer.ballots, ballot)
	s.ballotRefs[ballot.id] = ballot
}

func (s *state) addBlock(block *blockInfo) {
	layer := s.layer(block.layer)
	layer.blocks = append(layer.blocks, block)
	sortBlocks(layer.blocks)
	s.blockRefs[block.id] = block
	s.updateRefHeight(layer, block)
}

func (s *state) findRefHeightBelow(lid types.LayerID) uint64 {
	for lid = lid.Sub(1); lid.After(s.evicted); lid = lid.Sub(1) {
		layer := s.layer(lid)
		if len(layer.blocks) == 0 {
			continue
		}
		return layer.verifying.referenceHeight
	}
	return 0
}

func (s *state) updateRefHeight(layer *layerInfo, block *blockInfo) error {
	_, exist := s.referenceHeight[block.layer.GetEpoch()]
	if !exist {
		return fmt.Errorf("reference height for epoch %v wasn't computed", block.layer.GetEpoch())
	}
	if layer.verifying.referenceHeight == 0 && layer.lid.After(s.evicted) {
		layer.verifying.referenceHeight = s.findRefHeightBelow(layer.lid)
	}
	if block.height <= s.referenceHeight[block.layer.GetEpoch()] &&
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

func (b *ballotInfo) opinion() types.Hash32 {
	return b.votes.opinion()
}

type votes struct {
	tail *layerVote
}

func (v *votes) append(lv *layerVote) {
	if v.tail == nil {
		v.tail = lv
	} else {
		if v.tail.lid.Add(1) != lv.lid {
			panic("bug: added vote with a gap")
		}
		v.tail = v.tail.append(lv)
	}
	v.tail.sortSupported()
	v.tail.computeOpinion()
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

func (v *votes) opinion() types.Hash32 {
	if v.tail == nil {
		return types.Hash32{}
	}
	return v.tail.opinion
}

type layerVote struct {
	*layerInfo
	opinion   types.Hash32
	vote      sign
	supported []*blockInfo

	prev *layerVote
}

func (l *layerVote) getVote(bid types.BlockID) sign {
	for _, block := range l.supported {
		if block.id == bid {
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
	copied := l.copy()
	if copied.prev != nil {
		copied.prev = copied.prev.update(from, diff)
	}
	layerdiff, exist := diff[copied.lid]
	if exist && len(layerdiff) == 0 {
		copied.vote = abstain
		copied.supported = nil
	} else if exist && len(layerdiff) > 0 {
		var supported []*blockInfo
		for _, block := range copied.blocks {
			vote, exist := layerdiff[block.id]
			if exist && vote == against {
				continue
			}
			if (exist && vote == support) || l.getVote(block.id) == support {
				supported = append(supported, block)
			}
		}
		copied.supported = supported
		copied.sortSupported()
	}
	copied.computeOpinion()
	return copied
}

func (l *layerVote) sortSupported() {
	sortBlocks(l.supported)
}

func (l *layerVote) computeOpinion() {
	hasher := hash.New()
	if l.prev != nil {
		hasher.Write(l.prev.opinion[:])
	}
	if len(l.supported) > 0 {
		for _, block := range l.supported {
			hasher.Write(block.id[:])
		}
	} else if l.vote == abstain {
		hasher.Write(abstainSentinel)
	}
	hasher.Sum(l.opinion[:0])
}

func sortBlocks(blocks []*blockInfo) {
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].height < blocks[j].height {
			return true
		}
		return blocks[i].id.Compare(blocks[j].id)
	})
}
