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
	// weight of good ballots in the the layer N
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
	return v.goodBallots[ballot] != bad
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

	counted, goodBallotsCount := v.sumGoodBallots(logger, ballots)

	// TODO(dshulyak) counted weight should be reduced by the uncounted weight per conversation with research

	v.goodWeight[lid] = counted
	v.abstainedWeight[lid] = weightFromInt64(0)
	v.totalGoodWeight = v.totalGoodWeight.add(counted)

	logger.With().Info("counted weight from good ballots",
		log.Stringer("weight", counted),
		log.Stringer("total", v.totalGoodWeight),
		log.Stringer("expected", v.epochWeight[lid.GetEpoch()]),
		log.Int("ballots_count", len(ballots)),
		log.Int("good_ballots_count", goodBallotsCount),
	)
}

func (v *verifying) verify(logger log.Log, lid types.LayerID) bool {
	votingWeight := weightFromUint64(0).
		add(v.totalGoodWeight).
		sub(v.goodWeight[lid]).
		sub(v.abstainedWeight[lid])

	logger = logger.WithFields(
		log.String("verifier", verifyingTortoise),
		log.Stringer("candidate_layer", lid),
		log.Stringer("voting_weight", votingWeight),
		log.Stringer("asbtained_weight", v.abstainedWeight[lid]),
		log.Stringer("local_threshold", v.localThreshold),
		log.Stringer("global_threshold", v.globalThreshold),
	)

	// 0 - there is not enough weight to cross threshold.
	// 1 - layer is verified and contextual validity is according to our local opinion.
	if votingWeight.cmp(v.globalThreshold) == abstain {
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

func (v *verifying) sumGoodBallots(logger log.Log, ballots []tortoiseBallot) (weight, int) {
	sum := weightFromUint64(0)
	n := 0
	for _, ballot := range ballots {
		rst := v.determineGoodness(logger, ballot)
		if rst == bad {
			continue
		}
		v.goodBallots[ballot.id] = rst
		if rst == good || rst == abstained {
			sum = sum.add(ballot.weight)
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
	if len(ballot.abstain) > 0 {
		for lid := range ballot.abstain {
			v.abstainedWeight[lid].add(ballot.weight)
		}
		return abstained
	}

	if rst := v.goodBallots[ballot.base]; rst != good {
		logger.With().Debug("ballot can be good. only base is not good")
		return canBeGood
	}

	logger.With().Debug("ballot is good")
	return good
}
