package tortoise

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/spacemeshos/fixed"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/atxcache"
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
		// weight is a sum of all atxs
		weight weight
		// median height from atxs
		height uint64
		beacon *types.Beacon
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

		atxsdata *atxcache.Cache
		epochs   map[types.EpochID]*epochInfo
		layers   layerSlice
		// ballots should not be referenced by other ballots
		// each ballot stores references (votes) for X previous layers
		// those X layers may reference another set of ballots that will
		// reference recursively more layers with another set of ballots
		ballots map[types.LayerID][]*ballotInfo

		// to efficiently find base and reference ballots
		ballotRefs map[types.BallotID]*ballotInfo

		// malnodes is a collection with all nodes that equivocated in history.
		// each node id is 32 bytes. 100 000 of such nodes is only about ~3MB
		malnodes map[types.NodeID]struct{}
	}
)

func newState(atxdata *atxcache.Cache) *state {
	return &state{
		atxsdata:   atxdata,
		epochs:     map[types.EpochID]*epochInfo{},
		ballots:    map[types.LayerID][]*ballotInfo{},
		ballotRefs: map[types.BallotID]*ballotInfo{},
		malnodes:   map[types.NodeID]struct{}{},
	}
}

func (s *state) globalThreshold(cfg Config, target types.LayerID) weight {
	return computeGlobalThreshold(cfg, s.localThreshold, s.epochs, target, s.processed, s.last)
}

func (s *state) expectedWeight(cfg Config, target types.LayerID) weight {
	return computeExpectedWeightInWindow(cfg, s.epochs, target, s.processed, s.last)
}

func (s *state) layer(lid types.LayerID) *layerInfo {
	return s.layers.get(s.evicted, lid)
}

func (s *state) epoch(eid types.EpochID) *epochInfo {
	epoch, exist := s.epochs[eid]
	if !exist {
		epochsNumber.Inc()
		epoch = &epochInfo{}
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

func (s *state) getBlock(header types.Vote) *blockInfo {
	layer := s.layer(header.LayerID)
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

func (s *state) isMalfeasant(id types.NodeID) bool {
	_, exists := s.malnodes[id]
	return exists
}

func (s *state) markMalfeasant(id types.NodeID) {
	s.malnodes[id] = struct{}{}
}

type layerInfo struct {
	lid            types.LayerID
	empty          weight
	hareTerminated bool
	blocks         []*blockInfo
	verifying      verifyingInfo
	coinflip       sign

	// unique opinions recorded from the ballots in this layer.
	// ballot votes an opinion and encodes sidecar
	opinions map[types.Hash32]votes

	opinion types.Hash32
	// a pointer to the value stored on the previous layerInfo object
	// it is stored as a pointer so that when previous layerInfo is evicted
	// we still have access in case we need to recompute opinion for this layer
	prevOpinion *types.Hash32
}

func (l *layerInfo) computeOpinion(hdist uint32, last types.LayerID) {
	hasher := opinionhash.GetHasher()
	defer opinionhash.PutHasher(hasher)
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
		smesher         types.NodeID
		atxid           types.ATXID
		expectedBallots uint32
		beacon          types.Beacon
		weight          *big.Rat
		height          uint64
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

func (b *ballotInfo) overwriteOpinion(opinion types.Hash32) {
	b.votes.tail.opinion = opinion
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

func (v *votes) update(from types.LayerID, diff map[types.LayerID]map[types.BlockID]headerWithSign) (votes, error) {
	if v.tail == nil {
		return votes{}, nil
	}
	tail, err := v.tail.update(from, diff)
	if err != nil {
		return votes{}, err
	}
	return votes{tail: tail}, nil
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

func (l *layerVote) getVote(binfo *blockInfo) sign {
	for _, block := range l.supported {
		if block == binfo {
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

func (l *layerVote) update(
	from types.LayerID,
	diff map[types.LayerID]map[types.BlockID]headerWithSign,
) (*layerVote, error) {
	if l.lid.Before(from) {
		return l, nil
	}
	copied := l.copy()
	if copied.prev != nil {
		prev, err := copied.prev.update(from, diff)
		if err != nil {
			return nil, err
		}
		copied.prev = prev
	}
	layerdiff, exist := diff[copied.lid]
	if exist && len(layerdiff) == 0 {
		copied.vote = abstain
		copied.supported = nil
	} else if exist && len(layerdiff) > 0 {
		var supported []*blockInfo
		for _, block := range l.supported {
			vote, exist := layerdiff[block.id]
			if !exist {
				supported = append(supported, block)
			} else if vote.sign == against && vote.header != block.header() {
				return nil, fmt.Errorf("wrong target. last supported is (%s/%d), but is against (%s/%d)",
					block.id, block.height, vote.header.ID, vote.header.Height)
			}
		}
		for _, vote := range layerdiff {
			if vote.sign == against {
				continue
			}
			supported = append(supported, newBlockInfo(vote.header))
		}
		copied.supported = supported
		copied.sortSupported()
	}
	copied.computeOpinion()
	return copied, nil
}

func (l *layerVote) sortSupported() {
	sortBlocks(l.supported)
}

func (l *layerVote) computeOpinion() {
	hasher := opinionhash.GetHasher()
	defer opinionhash.PutHasher(hasher)
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

func newBlockInfo(header types.Vote) *blockInfo {
	return &blockInfo{
		id:     header.ID,
		layer:  header.LayerID,
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

	data bool // set to true if block is available locally
}

func (b *blockInfo) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddString("block", b.id.String())
	encoder.AddUint32("layer", b.layer.Uint32())
	encoder.AddUint64("height", b.height)
	return nil
}

func (b *blockInfo) header() types.Vote {
	return types.Vote{ID: b.id, LayerID: b.layer, Height: b.height}
}

type headerWithSign struct {
	header types.Vote
	sign   sign
}

func decodeVotes(evicted, blid types.LayerID, base *ballotInfo, exceptions types.Votes) (votes, types.LayerID, error) {
	from := base.layer
	diff := map[types.LayerID]map[types.BlockID]headerWithSign{}
	for _, header := range exceptions.Against {
		from = min(from, header.LayerID)
		layerdiff, exist := diff[header.LayerID]
		if !exist {
			layerdiff = map[types.BlockID]headerWithSign{}
			diff[header.LayerID] = layerdiff
		}
		existing, exist := layerdiff[header.ID]
		if exist {
			return votes{}, 0, fmt.Errorf(
				"conflicting votes on the same id %v with different heights %d conflict with %d",
				existing.header.ID,
				existing.header.Height,
				header.Height,
			)
		}
		layerdiff[header.ID] = headerWithSign{header, against}
	}
	for _, header := range exceptions.Support {
		from = min(from, header.LayerID)
		layerdiff, exist := diff[header.LayerID]
		if !exist {
			layerdiff = map[types.BlockID]headerWithSign{}
			diff[header.LayerID] = layerdiff
		}
		existing, exist := layerdiff[header.ID]
		if exist {
			return votes{}, 0, fmt.Errorf(
				"conflicting votes on the same id %v with different heights %d conflict with %d",
				existing.header.ID,
				existing.header.Height,
				header.Height,
			)
		}
		layerdiff[header.ID] = headerWithSign{header, support}
	}
	for _, lid := range exceptions.Abstain {
		from = min(from, lid)
		_, exist := diff[lid]
		if !exist {
			diff[lid] = map[types.BlockID]headerWithSign{}
		} else {
			return votes{}, 0, fmt.Errorf("votes on layer %d conflict with abstain", lid)
		}
	}
	// FIXME(dshulyak) this needs to be ignored when recovering from disk
	// if from <= evicted {
	// 	return votes{}, 0, fmt.Errorf("votes for a block in the layer (%d) outside the window (evicted %d)",
	// 	 from, evicted,
	// 	)
	// }

	// inherit opinion from the base ballot by copying votes
	decoded, err := base.votes.update(from, diff)
	if err != nil {
		return votes{}, 0, err
	}
	// add new opinions after the base layer
	for lid := base.layer; lid.Before(blid); lid = lid.Add(1) {
		lvote := layerVote{
			lid:  lid,
			vote: against,
		}
		layerdiff, exist := diff[lid]
		if exist && len(layerdiff) == 0 {
			lvote.vote = abstain
		} else if exist && len(layerdiff) > 0 {
			for _, vote := range layerdiff {
				if vote.sign != support {
					continue
				}
				lvote.supported = append(lvote.supported, newBlockInfo(vote.header))
			}
		}
		decoded.append(&lvote)
	}
	return decoded, from, nil
}

type layerSlice struct {
	data []*layerInfo
}

func (s *layerSlice) get(offset, index types.LayerID) *layerInfo {
	i := index - offset - 1
	lth := types.LayerID(len(s.data))
	if i < lth {
		return s.data[i]
	}
	last := offset + lth
	for lid := last + 1; lid <= index; lid++ {
		s.data = append(s.data, &layerInfo{lid: lid, opinions: map[types.Hash32]votes{}})
	}
	return s.data[i]
}

func (s *layerSlice) pop() {
	s.data = s.data[1:]
}
