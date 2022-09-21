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

func checkCanBeGood(ballot *ballotInfoV2) bool {
	return ballot.goodness == good || ballot.goodness == canBeGood
}

func newVerifying(config Config, state *state) *verifying {
	return &verifying{
		Config: config,
		state:  state,
	}
}

type verifying struct {
	Config
	*state

	// total weight of good ballots from verified + 1 up to last processed
	totalGoodWeight util.Weight
}

func (v *verifying) resetWeights() {
	v.totalGoodWeight = util.WeightFromUint64(0)
}

func (v *verifying) markGoodCut(logger log.Log, lid types.LayerID, ballots []ballotInfoV2) bool {
	n := 0
	for _, ballot := range ballots {
		if checkCanBeGood(ballot.base) && checkCanBeGood(&ballot) {
			logger.With().Debug("marking ballots that can be good as good",
				log.Stringer("ballot_layer", lid),
				log.Stringer("ballot", ballot.id),
				log.Stringer("base_ballot", ballot.base.id),
			)
			n++
		}
		ballot.base.goodness = good
		ballot.goodness = good
	}
	return n > 0
}

func (v *verifying) countVotes(logger log.Log, lid types.LayerID, ballots []ballotInfoV2) {
	logger = logger.WithFields(log.Stringer("ballots_layer", lid))

	goodWeight, goodBallotsCount := v.countGoodBallots(logger, lid, ballots)

	layer := v.layer(lid)
	layer.verifying.good = goodWeight
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

	layer := v.layer(lid)
	margin := util.WeightFromUint64(0).
		Add(v.totalGoodWeight).
		Sub(layer.verifying.good).
		Sub(layer.verifying.abstained)

	logger = logger.WithFields(
		log.String("verifier", verifyingTortoise),
		log.Stringer("candidate_layer", lid),
		log.Stringer("margin", margin),
		log.Stringer("abstained_weight", layer.verifying.abstained),
		log.Stringer("local_threshold", v.localThreshold),
		log.Stringer("global_threshold", v.globalThreshold),
	)
	if sign(margin.Cmp(v.globalThreshold)) == neutral {
		logger.With().Debug("doesn't cross global threshold")
		return false
	}
	if verifyLayer(
		logger,
		layer.blocks,
		func(block blockInfoV2) sign {
			if block.height > layer.verifying.referenceHeight {
				return neutral
			}
			decision, _ := getLocalVote(v.state, v.Config, &block)
			return decision
		},
	) {
		v.totalGoodWeight.Sub(layer.verifying.good)
		return true
	}
	return false
}

func (v *verifying) countGoodBallots(logger log.Log, lid types.LayerID, ballots []ballotInfoV2) (util.Weight, int) {
	sum := util.WeightFromUint64(0)
	n := 0
	for _, ballot := range ballots {
		if ballot.goodness == bad {
			continue
		}
		if ballot.weight.IsNil() {
			logger.With().Debug("ballot weight is nil", ballot.id)
			continue
		}
		// get height of the max votable block
		if refheight := v.layer(lid.Sub(1)).verifying.referenceHeight; refheight > ballot.height {
			logger.With().Debug("reference height is higher than the ballot height",
				ballot.id,
				log.Uint64("reference height", refheight),
				log.Uint64("ballot height", ballot.height),
			)
			continue
		}
		sum = sum.Add(ballot.weight)
		// abstained weight
		n++
	}
	return sum, n
}

func decodeExceptions(logger log.Log, config Config, state *state, base, child *ballotInfoV2, exceptions *types.Votes) {
	votes := copyVotes(state.evicted, base.votes)
	original := len(votes)
	for lid := base.layer; lid.Before(child.layer); lid = lid.Add(1) {
		layer := state.layer(lid)
		lvote := layerVote{
			layer: layer,
		}
		for i := range layer.blocks {
			block := &layer.blocks[i]
			lvote.blocks = append(lvote.blocks, blockVote{
				block: block,
				vote:  against,
			})
		}
		votes = append(votes, layerVote{
			layer: layer,
			vote:  against,
		})
	}
	child.goodness = good
	for _, bid := range exceptions.Support {
		block := state.blockRefs[bid]
		for i := len(votes) - 1; i >= 0; i-- {
			lvote := &votes[i]
			if lvote.layer.lid == block.layer {
				if lvote.layer.lid.Before(base.layer) {
					child.goodness = bad
				}
				for i := range lvote.blocks {
					bvote := &lvote.blocks[i]
					if bvote.block.id == bid {
						bvote.vote = support
					}
				}
				break
			}
		}
	}
	for _, bid := range exceptions.Against {
		block := state.blockRefs[bid]
		for i := len(votes) - 1; i >= 0; i-- {
			lvote := &votes[i]
			if lvote.layer.lid == block.layer {
				if lvote.layer.lid.Before(base.layer) {
					child.goodness = bad
				}
				for i := range lvote.blocks {
					bvote := &lvote.blocks[i]
					if bvote.block.id == bid {
						bvote.vote = against
					}
				}
				break
			}
		}
	}
	for _, lid := range exceptions.Abstain {
		child.goodness = abstained
		for i := len(votes) - 1; i >= 0; i-- {
			lvote := &votes[i]
			if lvote.layer.lid == lid {
				if lvote.layer.lid.Before(base.layer) {
					child.goodness = bad
				}
				lvote.vote = abstain
				break
			}
		}
	}
	if base.goodness != good && child.goodness == abstained {
		child.goodness = bad
	}
	if child.goodness != good {
		return
	}
	for i := range votes[original:] {
		lvote := &votes[original+i]
		for j := range lvote.blocks {
			bvote := &lvote.blocks[j]
			local, _ := getLocalVote(state, config, bvote.block)
			if bvote.vote != local {
				child.goodness = bad
				return
			}
		}
	}
	if base.goodness != good {
		child.goodness = canBeGood
	}
}
