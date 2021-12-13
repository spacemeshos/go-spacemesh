package tortoise

import (
	"container/list"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func newFullTortoise(config Config, common *commonState) *full {
	return &full{
		Config:       config,
		commonState:  common,
		votes:        map[types.BallotID]votes{},
		base:         map[types.BallotID]types.BallotID{},
		weights:      map[types.BlockID]weight{},
		delayedQueue: list.New(),
	}
}

type full struct {
	Config
	*commonState

	votes map[types.BallotID]votes
	base  map[types.BallotID]types.BallotID

	// counted weights up to this layer.
	//
	// counting votes is what makes full tortoise expensive during rerun.
	// we want to wait until verifying can't make progress before they are counted.
	// storing them in current version is cheap.
	counted types.LayerID
	// counted weights of all blocks up to counted layer.
	weights map[types.BlockID]weight
	// queue of the ballots with bad beacon
	delayedQueue *list.List
}

func (f *full) processBallots(ballots []tortoiseBallot) {
	for _, ballot := range ballots {
		f.base[ballot.id] = ballot.base
		f.votes[ballot.id] = ballot.votes
	}
}

func (f *full) processBlocks(blocks []*types.Block) {
	for _, block := range blocks {
		f.weights[block.ID()] = weightFromUint64(0)
	}
}

func (f *full) getVote(logger log.Log, ballot types.BallotID, block types.BlockID) sign {
	sign, exist := f.votes[ballot][block]
	if !exist {
		base, exist := f.base[ballot]
		if !exist {
			return against
		}
		return f.getVote(logger, base, block)
	}
	return sign
}

func (f *full) countVotesFromBallots(logger log.Log, ballotlid types.LayerID, ballots []types.BallotID) {
	var delayed []types.BallotID
	for _, ballot := range ballots {
		if !f.ballotFilter(ballot, ballotlid) {
			delayed = append(delayed, ballot)
			continue
		}
		ballotWeight := f.ballotWeight[ballot]
		for lid := f.verified.Add(1); lid.Before(ballotlid); lid = lid.Add(1) {
			for _, block := range f.blocks[lid] {
				vote := f.getVote(logger, ballot, block)
				current := f.weights[block]

				switch vote {
				case support:
					current = current.add(ballotWeight)
				case against:
					current = current.sub(ballotWeight)
				}
				f.weights[block] = current
			}
		}
	}
	if len(delayed) > 0 {
		f.delayedQueue.PushBack(delayedBallots{
			lid:     ballotlid,
			ballots: delayed,
		})
	}
}

func (f *full) countVotes(logger log.Log) {
	for front := f.delayedQueue.Front(); front != nil; {
		delayed := front.Value.(delayedBallots)
		if f.last.Difference(delayed.lid) <= f.BadBeaconVoteDelayLayers {
			break
		}
		logger.With().Debug("adding weight from delayed ballots", log.Stringer("ballots_layer", delayed.lid))

		f.countVotesFromBallots(logger.WithFields(log.Bool("delayed", true)), delayed.lid, delayed.ballots)

		next := front.Next()
		f.delayedQueue.Remove(front)
		front = next
	}
	for lid := f.counted.Add(1); !lid.After(f.processed); lid = lid.Add(1) {
		f.countVotesFromBallots(logger, lid, f.ballots[lid])
	}
	f.counted = f.processed
}

func (f *full) verify(logger log.Log, lid types.LayerID) bool {
	logger = logger.WithFields(
		log.Stringer("candidate_layer", lid),
		log.Stringer("threshold", f.threshold),
		log.Stringer("local_threshold", f.localThreshold),
		log.Stringer("global_threshold", f.globalThreshold),
	)

	blocks := f.blocks[lid]
	decisions := make([]sign, 0, len(blocks))

	for _, block := range blocks {
		current := f.weights[block]
		decision := current.cmp(f.threshold)
		if decision == abstain {
			logger.With().Info("candidate layer is not verified. block is undecided in full tortoise.",
				log.Stringer("block", block),
				log.Stringer("voting_weight", current),
			)
			return false
		}
		decisions = append(decisions, decision)
	}
	for i, block := range blocks {
		f.localVotes[block] = decisions[i]
	}
	logger.With().Info("candidate layer is verified by full tortoise")
	return true
}

// only ballots with the correct beacon value are considered good ballots and their votes counted by
// verifying tortoise. for ballots with a different beacon values, we count their votes only in self-healing mode
// and if they are old enough (by default using distance equal to an epoch).
func (f *full) ballotFilter(ballotID types.BallotID, ballotlid types.LayerID) bool {
	if _, bad := f.badBeaconBallots[ballotID]; !bad {
		return true
	}
	return f.last.Difference(ballotlid) > f.BadBeaconVoteDelayLayers
}

type delayedBallots struct {
	lid     types.LayerID
	ballots []types.BallotID
}
