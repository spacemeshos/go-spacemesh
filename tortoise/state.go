package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
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

func (s *state) addBlock(block *blockInfo) {
	layer := s.layer(block.layer)
	layer.blocks = append(layer.blocks, block)
	s.blockRefs[block.id] = block
	s.updateRefHeight(layer, block)
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

		votes votes

		goodness condition
	}
)

func (v *ballotInfo) updateBlockVote(block *blockInfo, vote sign) {
	for current := v.votes.tail; current != nil; current = current.prev {
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
	for current := v.votes.tail; current != nil; current = current.prev {
		if current.lid == lid {
			current.vote = vote
		}
	}
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

func (v *votes) copy() votes {
	if v.tail == nil {
		return votes{}
	}
	return votes{tail: v.tail}
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

type layerVote struct {
	*layerInfo
	vote   vote
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

func decodeExceptions(logger log.Log, config Config, state *state, base, ballot *ballotInfo, exceptions *types.Votes) {
	ballot.goodness |= base.goodness
	if _, exist := state.badBeaconBallots[ballot.id]; exist {
		ballot.goodness |= conditionBadBeacon
	}
	// inherit opinion from the base ballot by copying votes
	votes := base.votes.copy()
	// add opinions from the local state [base layer, ballot layer)
	for lid := base.layer; lid.Before(ballot.layer); lid = lid.Add(1) {
		layer := state.layer(lid)
		lvote := layerVote{
			layerInfo: layer,
			vote:      against,
		}
		for _, block := range layer.blocks {
			lvote.blocks = append(lvote.blocks, blockVote{
				blockInfo: block,
				vote:      against,
			})
		}
		votes.append(&lvote)
	}
	// update exceptions
	ballot.votes = votes
	for _, bid := range exceptions.Support {
		block, exist := state.blockRefs[bid]
		if !exist {
			ballot.goodness |= conditionVotesBeforeBase
			continue
		}
		if block.layer.Before(base.layer) {
			ballot.goodness |= conditionVotesBeforeBase
		}
		ballot.updateBlockVote(block, support)
	}
	for _, bid := range exceptions.Against {
		block, exist := state.blockRefs[bid]
		if !exist {
			ballot.goodness |= conditionVotesBeforeBase
			continue
		}
		if block.layer.Before(base.layer) {
			ballot.goodness |= conditionVotesBeforeBase
		}
		ballot.updateBlockVote(block, against)
	}
	for _, lid := range exceptions.Abstain {
		ballot.goodness |= conditionAbstained
		if lid.Before(base.layer) {
			ballot.goodness |= conditionVotesBeforeBase
		}
		ballot.updateLayerVote(lid, abstain)
		layer := state.layer(lid)
		layer.verifying.abstained = layer.verifying.abstained.Add(ballot.weight)
	}
	validateConsistency(config, state, ballot)
	logger.With().Debug("decoded votes for ballot",
		ballot.id,
		ballot.layer,
		log.Stringer("base", ballot.base.id),
		log.Uint32("base layer", ballot.base.layer.Value),
		log.Bool("base good", base.goodness.isGood()),
		log.Bool("ignored", ballot.goodness.ignored()),
		log.Bool("consistent", !ballot.goodness.notConsistent()),
	)
}

func validateConsistency(config Config, state *state, ballot *ballotInfo) bool {
	for lvote := ballot.votes.tail; lvote != nil; lvote = lvote.prev {
		if lvote.lid.Before(ballot.base.layer) {
			break
		}
		for j := range lvote.blocks {
			local, _ := getLocalVote(state, config, lvote.blocks[j].blockInfo)
			if vote := lvote.blocks[j].vote; vote != local {
				ballot.goodness |= conditionNotConsistent
				return false
			}
		}
	}
	return true
}
