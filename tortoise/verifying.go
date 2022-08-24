package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

type goodness uint8

func (g goodness) String() string {
	switch g {
	case bad:
		return "bad"
	case abstained:
		return "abstained"
	case good:
		return "good"
	case canBeGood:
		return "can be good"
	default:
		panic(fmt.Sprintf("unknown goodness %d", g))
	}
}

const (
	bad goodness = iota
	// ballots may vote abstain for a layer if the hare instance didn't terminate
	// for that layer. verifying tortoise counts weight from those ballots
	// but it doesn't count weight for abstained layers.
	//
	// for voting tortoise still prioritizes good ballots, ballots that selects base ballot
	// that abstained will be marked as a bad ballot. this condition is necessary
	// to simplify implementation. otherwise verifying tortoise needs to copy abstained
	// votes from base ballot if they tortoise hasn't terminated yet.
	//
	// Example:
	// Block: 0x01 Layer: 9
	// Ballot: 0xaa Layer: 11 Votes: {Base: 0x01, Support: [0x01], Abstain: [10]}
	// Assuming that Support is consistent with local opinion verifying tortoise
	// will count the weight from this ballot for layer 9, but not for layer 10.
	abstained
	// good ballot must:
	// - agree on a beacon value
	// - don't vote on blocks before base ballot
	// - have consistent votes with local opinion
	// - have a good base ballot.
	good
	// canBeGood is the same as good, but doesn't require good base ballot.
	canBeGood
)

func newVerifying(config Config, common *commonState) *verifying {
	return &verifying{
		Config:               config,
		commonState:          common,
		goodBallots:          map[types.BallotID]goodness{},
		goodWeight:           map[types.LayerID]util.Weight{},
		abstainedWeight:      map[types.LayerID]util.Weight{},
		totalGoodWeight:      util.WeightFromUint64(0),
		layerReferenceHeight: map[types.LayerID]uint64{},
	}
}

type verifying struct {
	Config
	*commonState

	goodBallots map[types.BallotID]goodness
	// weight of good ballots in the layer N
	goodWeight map[types.LayerID]util.Weight
	// abstained weight from ballots in layers [N+1, LAST]
	abstainedWeight map[types.LayerID]util.Weight
	// total weight of good ballots from verified + 1 up to last processed
	totalGoodWeight util.Weight
	// layerReferenceHeight is a highest block height below reference height
	layerReferenceHeight map[types.LayerID]uint64
}

func (v *verifying) onBlock(block *types.Block) {
	if block.TickHeight > v.referenceHeight[block.LayerIndex.GetEpoch()] {
		return
	}
	current, exist := v.layerReferenceHeight[block.LayerIndex]
	if !exist {
		current = v.layerReferenceHeight[block.LayerIndex.Sub(1)]
	}
	if block.TickHeight > current {
		current = block.TickHeight
	}
	v.layerReferenceHeight[block.LayerIndex] = current
}

func (v *verifying) getLayerReferenceHeight(lid types.LayerID) uint64 {
	return v.layerReferenceHeight[lid]
}

func (v *verifying) resetWeights() {
	v.totalGoodWeight = util.WeightFromUint64(0)
	v.goodWeight = map[types.LayerID]util.Weight{}
	v.abstainedWeight = map[types.LayerID]util.Weight{}
}

func (v *verifying) checkCanBeGood(ballot types.BallotID) bool {
	val := v.goodBallots[ballot]
	return val == good || val == canBeGood
}

func (v *verifying) markGoodCut(logger log.Log, lid types.LayerID, ballots []tortoiseBallot) bool {
	n := 0
	for _, ballot := range ballots {
		if v.checkCanBeGood(ballot.base) && v.checkCanBeGood(ballot.id) {
			logger.With().Debug("marking ballots that can be good as good",
				log.Stringer("ballot_layer", lid),
				log.Stringer("ballot", ballot.id),
				log.Stringer("base_ballot", ballot.base),
			)
			v.goodBallots[ballot.id] = good
			v.goodBallots[ballot.base] = good
			n++
		}
	}
	return n > 0
}

func (v *verifying) countVotes(logger log.Log, lid types.LayerID, ballots []tortoiseBallot) {
	logger = logger.WithFields(log.Stringer("ballots_layer", lid))

	goodWeight, goodBallotsCount := v.countGoodBallots(logger, lid, ballots)

	v.goodWeight[lid] = goodWeight
	v.totalGoodWeight = v.totalGoodWeight.Add(goodWeight)

	logger.With().Debug("counted weight from good ballots",
		log.Stringer("good_weight", goodWeight),
		log.Stringer("total_good_weight", v.totalGoodWeight),
		log.Stringer("expected", v.epochWeight[lid.GetEpoch()]),
		log.Int("ballots_count", len(ballots)),
		log.Int("good_ballots_count", goodBallotsCount),
	)
}

func (v *verifying) verify(logger log.Log, lid types.LayerID) bool {
	// totalGoodWeight      - weight from good ballots in layers [VERIFIED+1, LAST PROCESSED]
	// goodWeight[lid]      - weight from good ballots in the layer that is going to be verified
	// 					      expected to be VERIFIED + 1 (FIXME remove lid parameter)
	// abstainedWeight[lid] - weight from good ballots in layers (VERIFIED+1, LAST PROCESSED]
	//                        that abstained from voting for lid (VERIFIED + 1)
	// TODO(dshulyak) need to know correct values for threshold fractions before implementing next part
	// expectedWeight       - weight from (VERIFIED+1, LAST] layers,
	//                        this is the same weight that is used as a base for global threshold
	// uncountedWeight      - weight that was verifying tortoise didn't count
	//                        expectedWeight - (totalGoodWeight - goodWeight[lid])
	// margin               - this is pessimistic margin that is compared with global threshold
	//                        totalGoodWeight - goodWeight[lid] - abstainedWeight[lid] - uncountedWeight
	margin := util.WeightFromUint64(0).
		Add(v.totalGoodWeight).
		Sub(v.goodWeight[lid])
	if w, exist := v.abstainedWeight[lid]; exist {
		margin.Sub(w)
	}

	logger = logger.WithFields(
		log.String("verifier", verifyingTortoise),
		log.Stringer("candidate_layer", lid),
		log.Stringer("margin", margin),
		log.Stringer("abstained_weight", v.abstainedWeight[lid]),
		log.Stringer("local_threshold", v.localThreshold),
		log.Stringer("global_threshold", v.globalThreshold),
	)
	if sign(margin.Cmp(v.globalThreshold)) == neutral {
		logger.With().Debug("doesn't cross global threshold")
		return false
	}
	if verifyLayer(
		logger,
		v.blocks[lid],
		v.validity,
		func(block blockInfo) sign {
			if block.height > v.getLayerReferenceHeight(lid) {
				return neutral
			}
			decision, _ := getLocalVote(v.commonState, v.Config, lid, block.id)
			return decision
		},
	) {
		v.totalGoodWeight.Sub(v.goodWeight[lid])
		return true
	}
	return false
}

func (v *verifying) countGoodBallots(logger log.Log, lid types.LayerID, ballots []tortoiseBallot) (util.Weight, int) {
	sum := util.WeightFromUint64(0)
	n := 0
	for _, ballot := range ballots {
		if ballot.weight.IsNil() {
			logger.With().Debug("ballot weight is nil", ballot.id)
			continue
		}
		// get height of the max votable block
		if refheight := v.getLayerReferenceHeight(lid.Sub(1)); refheight > ballot.height {
			logger.With().Debug("reference height is higher than the ballot height",
				ballot.id,
				log.Uint64("reference height", refheight),
				log.Uint64("ballot height", ballot.height),
			)
			continue
		}
		rst := v.determineGoodness(logger, ballot)
		if rst == bad {
			continue
		}
		v.goodBallots[ballot.id] = rst
		if rst == good || rst == abstained {
			sum = sum.Add(ballot.weight)
			for lid := range ballot.abstain {
				if _, exist := v.abstainedWeight[lid]; !exist {
					v.abstainedWeight[lid] = util.WeightFromFloat64(0)
				}
				v.abstainedWeight[lid].Add(ballot.weight)
			}
			n++
		}
	}
	return sum, n
}

func (v *verifying) determineGoodness(logger log.Log, ballot tortoiseBallot) goodness {
	if _, exists := v.badBeaconBallots[ballot.id]; exists {
		logger.With().Debug("ballot is not good. ballot has a bad beacon")
		return bad
	}

	baselid := v.ballotLayer[ballot.base]

	for block, vote := range ballot.votes {
		blocklid, exists := v.blockLayer[block]
		// if the layer of the vote is not in the memory then it is definitely before base block layer
		if !exists || blocklid.Before(baselid) {
			logger.With().Debug("ballot is not good. vote on a block that is before base ballot",
				log.Stringer("base_layer", baselid),
				log.Stringer("vote_layer", blocklid),
				log.Bool("vote_exists", exists),
				ballot.id,
				log.Stringer("base", ballot.base),
			)
			return bad
		}

		if localVote, reason := getLocalVote(v.commonState, v.Config, blocklid, block); localVote != vote {
			logger.With().Debug("ballot is not good. vote on a block doesn't match a local vote",
				log.Stringer("local_vote", localVote),
				log.Stringer("local_vote_reason", reason),
				log.Stringer("ballot_vote", vote),
				log.Stringer("block_layer", blocklid),
				log.Stringer("base_layer", baselid),
				log.Stringer("block", block),
				ballot.id,
				log.Stringer("base", ballot.base),
			)
			return bad
		}
	}

	base := v.goodBallots[ballot.base]

	if len(ballot.abstain) > 0 {
		logger.With().Debug("ballot has abstained on some layers",
			ballot.id,
			log.Stringer("base", ballot.base),
			log.Stringer("base_goodness", base))
		switch base {
		case good:
			return abstained
		default:
			return bad
		}
	}

	if base != good {
		logger.With().Debug("ballot can be good. only base is not good",
			ballot.id,
			log.Stringer("base", ballot.base))
		return canBeGood
	}

	logger.With().Debug("ballot is good",
		ballot.id,
		log.Stringer("base", ballot.base),
	)
	return good
}
