package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func newVerifying(logger log.Log, config Config, common *commonState) *verifying {
	return &verifying{
		logger:       logger.Named("verifying"),
		Config:       config,
		commonState:  common,
		goodBallots:  map[types.BallotID]struct{}{},
		layerWeights: map[types.LayerID]weight{},
	}
}

type verifying struct {
	logger log.Log

	Config
	*commonState

	goodBallots map[types.BallotID]struct{}
	// weight of good ballots in the layer
	layerWeights map[types.LayerID]weight
	// total weight of good ballots from verified + 1 up to last
	totalWeight weight
}

func (v *verifying) processLayer(lid types.LayerID, localOpinion Opinion, ballots []tortoiseBallot) types.LayerID {
	logger := v.logger.WithFields(lid)

	expected := v.epochWeight[lid.GetEpoch()]
	counted, count := v.sumGoodBallots(logger, localOpinion, ballots)

	uncounted := weightFromUint64(0)
	uncounted = uncounted.add(expected)
	uncounted = uncounted.sub(counted)

	counted = counted.sub(uncounted)

	v.layerWeights[lid] = counted
	v.totalWeight = v.totalWeight.add(counted)

	logger.With().Info("computed weight from good ballots",
		log.Stringer("weight", counted),
		log.Stringer("total", v.totalWeight),
		log.Stringer("expected", expected),
		log.Stringer("uncounted", uncounted),
		log.Int("ballots_count", len(ballots)),
		log.Int("good_ballots_count", count),
	)
	if count > 0 {
		v.good = lid
	}

	return v.verifyLayers(logger, localOpinion)
}

func (v *verifying) verifyLayers(logger log.Log, localOpinion Opinion) types.LayerID {
	localThreshold := weightFromUint64(0)
	localThreshold = localThreshold.add(v.epochWeight[v.last.GetEpoch()])
	localThreshold = localThreshold.fraction(v.LocalThreshold)

	for lid := v.verified.Add(1); lid.Before(v.processed); lid = lid.Add(1) {
		// TODO(dshulyak) expected weight must be based always on the last layer.
		threshold := computeExpectedWeight(v.epochWeight, lid, v.processed)
		threshold = threshold.fraction(v.GlobalThreshold)
		threshold = threshold.add(localThreshold)

		votingWeight := weightFromUint64(0)
		votingWeight = votingWeight.add(v.totalWeight)
		votingWeight = votingWeight.sub(v.layerWeights[lid])

		logger = logger.WithFields(
			log.Stringer("candidate_layer", lid),
			log.Stringer("voting_weight", votingWeight),
			log.Stringer("threshold", threshold),
		)

		// 0 - there is not enough weight to cross threshold.
		// 1 - layer is verified and contextual validity is according to our local opinion.
		if votingWeight.cmp(threshold) == abstain {
			logger.With().Warning("candidate layer is not verified")
			return lid.Sub(1)
		}

		// if there is any block with neutral local opinion - we can't verify the layer
		// if it happens outside of hdist - protocol will switch to full tortoise
		for _, bid := range v.blocks[lid] {
			if localOpinion[bid] == abstain {
				return lid.Sub(1)
			}
		}

		v.totalWeight = votingWeight
		logger.With().Info("candidate layer is verified")
	}
	return v.last.Sub(1)
}

func (v *verifying) sumGoodBallots(logger log.Log, localOpinion map[types.BlockID]sign, ballots []tortoiseBallot) (weight, int) {
	sum := weightFromUint64(0)
	n := 0
	for _, ballot := range ballots {
		if !v.isGood(logger, localOpinion, ballot) {
			continue
		}
		v.goodBallots[ballot.id] = struct{}{}
		sum = sum.add(ballot.weight)
		n++
	}
	return sum, n
}

func (v *verifying) isGood(logger log.Log, localOpinion Opinion, ballot tortoiseBallot) bool {
	logger = logger.WithFields(ballot.id, log.Stringer("base", ballot.base))

	if _, exists := v.badBeaconBallots[ballot.id]; exists {
		logger.With().Debug("ballot has a bad beacon")
		return false
	}

	if _, exists := v.goodBallots[ballot.base]; !exists {
		logger.With().Debug("base ballot is not good")
		return false
	}

	baselid := v.ballotLayer[ballot.base]
	for id, sign := range ballot.votes {
		votelid, exists := v.blockLayer[id]
		// if the layer of the vote is not in the memory then it is definitely before base block layer
		if !exists || votelid.Before(baselid) {
			logger.With().Debug("vote on a block before base block",
				log.Stringer("base_layer", baselid),
				log.Stringer("vote_layer", votelid),
				log.Bool("vote_exists", exists),
			)
			return false
		}

		if localOpinion[id] != sign {
			logger.With().Debug("vote on block is different from the local vote",
				log.Stringer("local_vote", localOpinion[id]),
				log.Stringer("vote", sign),
			)
			return false
		}
	}

	logger.With().Debug("ballot is good", ballot.id)
	return true
}
