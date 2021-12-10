package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func newVerifying(config Config, common *commonState) *verifying {
	return &verifying{
		Config:       config,
		commonState:  common,
		goodBallots:  map[types.BallotID]struct{}{},
		layerWeights: map[types.LayerID]weight{},
		totalWeight:  weightFromUint64(0),
	}
}

type verifying struct {
	Config
	*commonState

	goodBallots map[types.BallotID]struct{}
	// weight of good ballots in the layer
	layerWeights map[types.LayerID]weight
	// total weight of good ballots from verified + 1 up to last processed
	totalWeight weight
}

func (v *verifying) processLayer(logger log.Log, lid types.LayerID, ballots []tortoiseBallot) {
	counted, count := v.sumGoodBallots(logger, ballots)

	// TODO(dshulyak) counted weight should be reduced by the uncounted weight per conversation with research

	v.layerWeights[lid] = counted
	v.totalWeight = v.totalWeight.add(counted)

	logger.With().Info("computed weight from good ballots",
		log.Stringer("weight", counted),
		log.Stringer("total", v.totalWeight),
		log.Stringer("expected", v.epochWeight[lid.GetEpoch()]),
		log.Int("ballots_count", len(ballots)),
		log.Int("good_ballots_count", count),
	)
}

func (v *verifying) verifyLayers(logger log.Log) types.LayerID {
	localThreshold := computeLocalThreshold(v.Config, v.epochWeight, v.processed)

	for lid := v.verified.Add(1); lid.Before(v.processed); lid = lid.Add(1) {
		// TODO(dshulyak) expected weight must be based on the last layer.
		threshold := computeGlobalThreshold(v.Config, v.epochWeight, lid, v.processed)
		threshold = threshold.add(localThreshold)

		votingWeight := weightFromUint64(0)
		votingWeight = votingWeight.add(v.totalWeight)
		votingWeight = votingWeight.sub(v.layerWeights[lid])

		layerLogger := logger.WithFields(
			log.Stringer("candidate_layer", lid),
			log.Stringer("voting_weight", votingWeight),
			log.Stringer("threshold", threshold),
			log.Stringer("local_threshold", localThreshold),
		)

		// 0 - there is not enough weight to cross threshold.
		// 1 - layer is verified and contextual validity is according to our local opinion.
		if votingWeight.cmp(threshold) == abstain {
			layerLogger.With().Warning("candidate layer is not verified. voting weight is lower than the threshold")
			return lid.Sub(1)
		}

		// if there is any block with neutral local opinion - we can't verify the layer
		// if it happens outside of hdist - protocol will switch to full tortoise
		for _, bid := range v.blocks[lid] {
			if v.localOpinion[bid] == abstain {
				layerLogger.With().Warning("candidate layer is not verified. block is undecided", bid)
				return lid.Sub(1)
			}
		}

		v.totalWeight = votingWeight
		layerLogger.With().Info("candidate layer is verified")
	}
	return v.processed.Sub(1)
}

func (v *verifying) sumGoodBallots(logger log.Log, ballots []tortoiseBallot) (weight, int) {
	sum := weightFromUint64(0)
	n := 0
	for _, ballot := range ballots {
		if !v.isGood(logger, ballot) {
			continue
		}
		v.goodBallots[ballot.id] = struct{}{}
		sum = sum.add(ballot.weight)
		n++
	}
	return sum, n
}

func (v *verifying) isGood(logger log.Log, ballot tortoiseBallot) bool {
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

		if local := v.localOpinion[id]; local != sign {
			logger.With().Debug("vote on block is different from the local vote",
				log.Stringer("local_vote", local),
				log.Stringer("vote", sign),
				log.Stringer("block_layer", votelid),
			)
			return false
		}
	}

	logger.With().Debug("ballot is good", ballot.id)
	return true
}
