package tortoise

import (
	"math/big"
	"sort"

	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise/opinionhash"
)

type (
	weight = fixed.Fixed

	verifyingInfo struct {
		// goodUncounted is a weight that doesn't vote for this layer
		//
		// for example ballot created in layer 10 doesn't vote for layers
		// 10 and above, therefore its weight needs to be added to goodUncounted
		// for layers 10 and above
		goodUncounted   weight
		referenceHeight uint64
	}

	epochInfo struct {
		atxs map[types.ATXID]uint64
		// weight is a sum of all atxs
		weight weight
		// median height from atxs
		height uint64
	}

	state struct {
		// last received layer
		// TODO should be last layer according to the clock
		// https://github.com/spacemeshos/go-spacemesh/issues/2921
		last types.LayerID
		// localThreshold is updated together with the last layer.
		localThreshold weight

		// last verified layer
		verified types.LayerID
		// last processed layer
		processed types.LayerID
		// last evicted layer
		evicted types.LayerID

		changedOpinion struct {
			// sector of layers where opinion is different from previously computed opinion
			min, max types.LayerID
		}

		epochs map[types.EpochID]*epochInfo
		layers map[types.LayerID]*layerInfo
		// ballots should not be referenced by other ballots
		// each ballot stores references (votes) for X previous layers
		// those X layers may reference another set of ballots that will
		// reference recursively more layers with another set of ballots
		ballots map[types.LayerID][]*ballotInfo

		// to efficiently find base and reference ballots
		ballotRefs map[types.BallotID]*ballotInfo
	}
)

func newState() *state {
	return &state{
		epochs:     map[types.EpochID]*epochInfo{},
		layers:     map[types.LayerID]*layerInfo{},
		ballots:    map[types.LayerID][]*ballotInfo{},
		ballotRefs: map[types.BallotID]*ballotInfo{},
	}
}

func (s *state) globalThreshold(cfg Config, target types.LayerID) weight {
	return computeGlobalThreshold(cfg, s.localThreshold, s.epochs, target, s.processed, s.last)
}

func (s *state) expectedWeight(cfg Config, target types.LayerID) weight {
	return computeExpectedWeightInWindow(cfg, s.epochs, target, s.processed, s.last)
}

func (s *state) layer(lid types.LayerID) *layerInfo {
	layer, exist := s.layers[lid]
	if !exist {
		layersNumber.Inc()
		layer = &layerInfo{lid: lid}
		s.layers[lid] = layer
	}
	return layer
}

func (s *state) epoch(eid types.EpochID) *epochInfo {
	epoch, exist := s.epochs[eid]
	if !exist {
		epochsNumber.Inc()
		epoch = &epochInfo{atxs: map[types.ATXID]uint64{}}
		s.epochs[eid] = epoch
	}
	return epoch
}

func (s *state) addBallot(ballot *ballotInfo) {
	ballotsNumber.Inc()
	s.ballots[ballot.layer] = append(s.ballots[ballot.layer], ballot)
	s.ballotRefs[ballot.id] = ballot
}

func (s *state) addBlock(block *blockInfo) {
	blocksNumber.Inc()
	layer := s.layer(block.layer)
	if layer.hareTerminated {
		block.hare = against
	}
	layer.blocks = append(layer.blocks, block)
	sortBlocks(layer.blocks)
	s.updateRefHeight(layer, block)
}

func (s *state) getBlock(header types.BlockHeader) *blockInfo {
	layer := s.layer(header.Layer)
	for _, block := range layer.blocks {
		if block.id == header.ID && block.height == header.Height {
			return block
		}
	}
	return nil
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

func (s *state) updateRefHeight(layer *layerInfo, block *blockInfo) {
	epoch, exist := s.epochs[block.layer.GetEpoch()]
	if !exist {
		return
	}
	if layer.verifying.referenceHeight == 0 && layer.lid.After(s.evicted) {
		layer.verifying.referenceHeight = s.findRefHeightBelow(layer.lid)
	}
	if block.height <= epoch.height &&
		block.height > layer.verifying.referenceHeight {
		layer.verifying.referenceHeight = block.height
	}
}

type layerInfo struct {
	lid            types.LayerID
	empty          weight
	hareTerminated bool
	blocks         []*blockInfo
	verifying      verifyingInfo

	opinion types.Hash32
	// a pointer to the value stored on the previous layerInfo object
	// it is stored as a pointer so that when previous layerInfo is evicted
	// we still have access in case we need to recompute opinion for this layer
	prevOpinion *types.Hash32
}

func (l *layerInfo) computeOpinion(hdist uint32, last types.LayerID) {
	hasher := opinionhash.New()
	if l.prevOpinion != nil {
		hasher.WritePrevious(*l.prevOpinion)
	}
	if !l.hareTerminated {
		hasher.WriteAbstain()
	} else if withinDistance(hdist, l.lid, last) {
		for _, block := range l.blocks {
			if block.hare == support {
				hasher.WriteSupport(block.id, block.height)
			}
		}
	} else {
		for _, block := range l.blocks {
			if block.validity == support {
				hasher.WriteSupport(block.id, block.height)
			}
		}
	}
	hasher.Sum(l.opinion[:0])
}

type (
	baseInfo struct {
		id    types.BallotID
		layer types.LayerID
	}

	conditions struct {
		// set after comparing with local beacon
		badBeacon bool
	}

	referenceInfo struct {
		weight *big.Rat
		height uint64
		beacon types.Beacon
	}

	ballotInfo struct {
		id         types.BallotID
		layer      types.LayerID
		base       baseInfo
		malicious  bool
		weight     weight
		reference  *referenceInfo
		votes      votes
		conditions conditions
	}
)

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

func (v *votes) update(from types.LayerID, diff map[types.LayerID]map[types.BlockHeader]sign) votes {
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

func (v *votes) opinion() types.Hash32 {
	if v.tail == nil {
		return types.Hash32{}
	}
	return v.tail.opinion
}

type layerVote struct {
	lid       types.LayerID
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
		lid:       l.lid,
		vote:      l.vote,
		supported: l.supported,
		prev:      l.prev,
	}
}

func (l *layerVote) append(lv *layerVote) *layerVote {
	lv.prev = l
	return lv
}

func (l *layerVote) update(from types.LayerID, diff map[types.LayerID]map[types.BlockHeader]sign) *layerVote {
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
		for _, block := range l.supported {
			header := block.header()
			vote, exist := layerdiff[header]
			if exist && vote == against {
				continue
			}
			supported = append(supported, block)
			if exist {
				delete(layerdiff, header)
			}
		}
		for header, vote := range layerdiff {
			if vote == against {
				continue
			}
			supported = append(supported, newBlockInfo(header))
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
	hasher := opinionhash.New()
	if l.prev != nil {
		hasher.WritePrevious(l.prev.opinion)
	}
	if len(l.supported) > 0 {
		for _, block := range l.supported {
			hasher.WriteSupport(block.id, block.height)
		}
	} else if l.vote == abstain {
		hasher.WriteAbstain()
	}
	hasher.Sum(l.opinion[:0])
}

func sortBlocks(blocks []*blockInfo) {
	sort.Slice(blocks, func(i, j int) bool {
		if blocks[i].height != blocks[j].height {
			return blocks[i].height < blocks[j].height
		}
		return blocks[i].id.Compare(blocks[j].id)
	})
}

func newBlockInfo(header types.BlockHeader) *blockInfo {
	return &blockInfo{
		id:     header.ID,
		layer:  header.Layer,
		height: header.Height,
		hare:   neutral,
	}
}

type blockInfo struct {
	id     types.BlockID
	layer  types.LayerID
	height uint64
	hare   sign
	margin weight

	validity sign
	emitted  sign // same as validity field if event was emitted

	data bool // set to true if block is available locally
}

func (b *blockInfo) header() types.BlockHeader {
	return types.BlockHeader{ID: b.id, Layer: b.layer, Height: b.height}
}
