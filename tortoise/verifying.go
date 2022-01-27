package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
		Config:          config,
		commonState:     common,
		goodBallots:     map[types.BallotID]goodness{},
		goodWeight:      map[types.LayerID]weight{},
		abstainedWeight: map[types.LayerID]weight{},
		totalGoodWeight: weightFromUint64(0),
	}
}

type verifying struct {
	Config
	*commonState

	goodBallots map[types.BallotID]goodness
	// weight of good ballots in the layer N
	goodWeight map[types.LayerID]weight
	// abstained weight from ballots in layers [N+1, LAST]
	abstainedWeight map[types.LayerID]weight
	// total weight of good ballots from verified + 1 up to last processed
	totalGoodWeight weight
}

func (v *verifying) resetWeights() {
	v.totalGoodWeight = weightFromUint64(0)
	v.goodWeight = map[types.LayerID]weight{}
	v.abstainedWeight = map[types.LayerID]weight{}
}

func (v *verifying) checkCanBeGood(ballot types.BallotID) bool {
	val := v.goodBallots[ballot]
	return val == good || val == canBeGood
}

func (v *verifying) markGoodCut(logger log.Log, lid types.LayerID, ballots []tortoiseBallot) bool {
	n := 0
	for _, ballot := range ballots {
		if v.checkCanBeGood(ballot.base) && v.checkCanBeGood(ballot.id) {
			logger.With().Info("marking ballots that can be good as good",
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

	goodWeight, goodBallotsCount := v.countGoodBallots(logger, ballots)

	v.goodWeight[lid] = goodWeight
	v.totalGoodWeight = v.totalGoodWeight.add(goodWeight)

	logger.With().Info("counted weight from good ballots",
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
	margin := weightFromUint64(0).
		add(v.totalGoodWeight).
		sub(v.goodWeight[lid])
	if w, exist := v.abstainedWeight[lid]; exist {
		margin.sub(w)
	}

	logger = logger.WithFields(
		log.String("verifier", verifyingTortoise),
		log.Stringer("candidate_layer", lid),
		log.Stringer("margin", margin),
		log.Stringer("abstained_weight", v.abstainedWeight[lid]),
		log.Stringer("local_threshold", v.localThreshold),
		log.Stringer("global_threshold", v.globalThreshold),
	)

	// 0 - there is not enough weight to cross threshold.
	// 1 - layer is verified and contextual validity is according to our local opinion.
	if margin.cmp(v.globalThreshold) == abstain {
		logger.With().Info("candidate layer is not verified." +
			" voting weight from good ballots is lower than the threshold")
		return false
	}

	// if there is any block with neutral local opinion - we can't verify the layer
	// if it happens outside hdist - protocol will switch to full tortoise
	for _, bid := range v.blocks[lid] {
		vote, _ := getLocalVote(v.commonState, v.Config, lid, bid)
		if vote == abstain {
			logger.With().Info("candidate layer is not verified."+
				" block is undecided according to local votes",
				bid,
			)
			return false
		}
		v.validity[bid] = vote
	}

	v.totalGoodWeight.sub(v.goodWeight[lid])
	logger.With().Info("candidate layer is verified")
	return true
}

func (v *verifying) countGoodBallots(logger log.Log, ballots []tortoiseBallot) (weight, int) {
	sum := weightFromUint64(0)
	n := 0
	for _, ballot := range ballots {
		if ballot.weight.isNil() {
			continue
		}
		rst := v.determineGoodness(logger, ballot)
		if rst == bad {
			continue
		}
		v.goodBallots[ballot.id] = rst
		if rst == good || rst == abstained {
			sum = sum.add(ballot.weight)
			for lid := range ballot.abstain {
				if _, exist := v.abstainedWeight[lid]; !exist {
					v.abstainedWeight[lid] = weightFromFloat64(0)
				}
				v.abstainedWeight[lid].add(ballot.weight)
			}
			n++
		}
	}
	return sum, n
}

func (v *verifying) determineGoodness(logger log.Log, ballot tortoiseBallot) goodness {
	logger = logger.WithFields(ballot.id, log.Stringer("base", ballot.base))

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
			)
			return bad
		}
	}

	base := v.goodBallots[ballot.base]

	if len(ballot.abstain) > 0 {
		logger.With().Debug("ballot has abstained on some layers", log.Stringer("base_goodness", base))
		switch base {
		case good:
			return abstained
		default:
			return bad
		}
	}

	if base != good {
		logger.With().Debug("ballot can be good. only base is not good")
		return canBeGood
	}

	logger.With().Debug("ballot is good")
	return good
}
