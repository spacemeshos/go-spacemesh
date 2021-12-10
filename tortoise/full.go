package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func newFullTortoise(config Config, common *commonState) *full {
	return &full{
		Config:      config,
		commonState: common,
		votes:       map[types.BallotID]Opinion{},
		base:        map[types.BallotID]types.BallotID{},
	}
}

type full struct {
	Config
	*commonState

	votes map[types.BallotID]Opinion
	base  map[types.BallotID]types.BallotID
}

func (f *full) processBallots(ballots []tortoiseBallot) {
	for _, ballot := range ballots {
		f.base[ballot.id] = ballot.base
		f.votes[ballot.id] = ballot.votes
	}
}

func (f *full) getVote(logger log.Log, ballot types.BallotID, block types.BlockID) sign {
	sign, exist := f.votes[ballot][block]
	if !exist {
		base, exist := f.base[ballot]
		if !exist {
			logger.With().Error("bug: should not be requesting vote for block that is not reachable", block, ballot)
			return abstain
		}
		return f.getVote(logger, base, block)
	}
	return sign
}

// only ballots with the correct beacon value are considered good ballots and their votes counted by
// verifying tortoise. for ballots with a different beacon values, we count their votes only in self-healing mode
// if they are from previous epoch.
func (f *full) ballotFilter(ballotID types.BallotID, ballotlid types.LayerID) bool {
	if _, bad := f.badBeaconBallots[ballotID]; !bad {
		return true
	}
	return f.last.Difference(ballotlid) > f.BadBeaconVoteDelayLayers
}

func (f *full) sumVotesForBlock(logger log.Log, blockID types.BlockID, startlid types.LayerID) (weight, error) {
	// TODO(dshulyak) we can count votes from ballots once they are received and avoid
	// repeating work over and over again. one complexity is to delay votes from ballots with
	// bad beacon.

	var (
		sum = weightFromUint64(0)
		end = f.processed
	)
	logger = logger.WithFields(
		log.Stringer("start_layer", startlid),
		log.Stringer("end_layer", end),
		log.Stringer("block_voting_on", blockID),
		log.Stringer("layer_voting_on", startlid.Sub(1)),
	)
	for votelid := startlid; !votelid.After(end); votelid = votelid.Add(1) {
		logger := logger.WithFields(votelid)
		for _, ballotID := range f.ballots[votelid] {
			if !f.ballotFilter(ballotID, votelid) {
				logger.With().Debug("voting block did not pass filter, not counting its vote", ballotID)
				continue
			}
			sign := f.getVote(logger, ballotID, blockID)

			ballotWeight, exists := f.ballotWeight[ballotID]
			if !exists {
				return weightFromUint64(0), fmt.Errorf("bug: weight for %s is not computed", ballotID)
			}
			adjusted := ballotWeight
			if sign == against {
				// copy is needed only if we modify sign
				adjusted = ballotWeight.copy().neg()
			}
			if sign != abstain {
				sum = sum.add(adjusted)
			}
		}
	}
	return sum, nil
}

// Manually count all votes for all layers since the last verified layer, up to the newly-arrived layer (there's no
// point in going further since we have no new information about any newer layers). Self-healing does not take into
// consideration local opinion, it relies solely on global opinion.
func (f *full) verifyLayers(logger log.Log) types.LayerID {
	localThreshold := computeLocalThreshold(f.Config, f.epochWeight, f.processed)

	for lid := f.verified.Add(1); lid.Before(f.processed); lid = lid.Add(1) {
		// TODO(dshulyak) computation for threshold should use last as an upper boundary
		threshold := computeGlobalThreshold(f.Config, f.epochWeight, lid, f.processed)
		threshold = threshold.add(localThreshold)

		layerLogger := logger.WithFields(
			log.Named("target_layer", f.processed),
			log.Named("candidate_layer", lid),
			log.Named("last_layer", f.last),
			log.Stringer("threshold", threshold),
			log.Stringer("local_threshold", localThreshold),
		)

		logger.Info("verify candidate layer with full tortoise")

		// record the contextual validity for all blocks in this layer
		for _, blockID := range f.blocks[lid] {
			blockLogger := layerLogger.WithFields(log.Stringer("candidate_block_id", blockID))

			// count all votes for or against this block by all blocks in later layers. for ballots with a different
			// beacon values, we delay their votes by badBeaconVoteDelays layers
			sum, err := f.sumVotesForBlock(blockLogger, blockID, lid.Add(1))
			if err != nil {
				logger.With().Error("error summing votes for candidate block in candidate layer", log.Err(err))
				return lid.Sub(1)
			}

			// check that the total weight exceeds the global threshold
			sign := sum.cmp(threshold)
			blockLogger.With().Debug("computed global opinion on candidate block from votes",
				log.Stringer("global_opinion", sign),
				log.Stringer("vote_sum", sum),
			)

			if sign == abstain {
				blockLogger.With().Info("full tortoise failed to verify candidate layer",
					log.Stringer("global_opinion", sign),
					log.Stringer("vote_sum", sum),
					blockID,
				)
				return lid.Sub(1)
			}
			for _, blockID := range f.blocks[lid] {
				f.localOpinion[blockID] = sign
			}
		}

		logger.Info("full tortoise verified candidate layer")
	}
	return f.processed.Sub(1)
}
