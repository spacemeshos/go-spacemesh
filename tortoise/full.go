package tortoise

import (
	"container/list"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

func newFullTortoise(config Config, common *commonState) *full {
	return &full{
		Config:       config,
		commonState:  common,
		votes:        map[types.BallotID]votes{},
		abstain:      map[types.BallotID]map[types.LayerID]struct{}{},
		base:         map[types.BallotID]types.BallotID{},
		empty:        map[types.LayerID]util.Weight{},
		delayedQueue: list.New(),
	}
}

type full struct {
	Config
	*commonState

	votes   map[types.BallotID]votes
	abstain map[types.BallotID]map[types.LayerID]struct{}
	base    map[types.BallotID]types.BallotID

	// weight of the ballots that vote that a particular layer is empty
	empty map[types.LayerID]util.Weight
	// counted weights up to this layer.
	//
	// counting votes is what makes full tortoise expensive during rerun.
	// we want to wait until verifying can't make progress before they are counted.
	// storing them in current version is cheap.
	counted types.LayerID
	// queue of the ballots with bad beacon
	delayedQueue *list.List
}

func (f *full) processBallots(ballots []tortoiseBallot) {
	for _, ballot := range ballots {
		f.base[ballot.id] = ballot.base
		f.votes[ballot.id] = ballot.votes
		f.abstain[ballot.id] = ballot.abstain
	}
}

func (f *full) onBallot(ballot *tortoiseBallot) {
	f.base[ballot.id] = ballot.base
	f.votes[ballot.id] = ballot.votes
	f.abstain[ballot.id] = ballot.abstain
}

func (f *full) getVote(logger log.Log, ballot types.BallotID, blocklid types.LayerID, block types.BlockID) sign {
	sign, exist := f.votes[ballot][block]
	if !exist {
		_, exist = f.abstain[ballot][blocklid]
		if exist {
			return abstain
		}

		base, exist := f.base[ballot]
		if !exist {
			return against
		}
		return f.getVote(logger, base, blocklid, block)
	}
	return sign
}

func (f *full) abstained(ballot types.BallotID, blocklid types.LayerID) bool {
	_, exist := f.abstain[ballot][blocklid]
	if exist {
		return true
	}
	base, exist := f.base[ballot]
	if !exist {
		return false
	}
	return f.abstained(base, blocklid)
}

func (f *full) countVotesFromBallots(logger log.Log, ballotlid types.LayerID, ballots []ballotInfo) {
	var delayed []ballotInfo
	for _, ballot := range ballots {
		if f.shouldBeDelayed(ballot.id, ballotlid) {
			delayed = append(delayed, ballot)
			continue
		}
		if ballot.weight.IsNil() {
			continue
		}
		for lid := f.verified.Add(1); lid.Before(ballotlid); lid = lid.Add(1) {
			if _, exist := f.empty[lid]; !exist {
				f.empty[lid] = util.WeightFromUint64(0)
			}
			blocks := f.blocks[lid]
			empty := true
			for i := range blocks {
				block := &blocks[i]
				if block.height > ballot.height {
					continue
				}
				switch f.getVote(logger, ballot.id, lid, block.id) {
				case support:
					empty = false
					block.weight.Add(ballot.weight)
				case against:
					block.weight.Sub(ballot.weight)
				}
			}
			if empty && !f.abstained(ballot.id, lid) {
				f.empty[lid].Add(ballot.weight)
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

func (f *full) countLayerVotes(logger log.Log, lid types.LayerID) {
	if !lid.After(f.counted) {
		return
	}
	f.counted = lid

	for front := f.delayedQueue.Front(); front != nil; {
		delayed := front.Value.(delayedBallots)
		if f.last.Difference(delayed.lid) <= f.BadBeaconVoteDelayLayers {
			break
		}
		logger.With().Debug("adding weight from delayed ballots",
			log.Stringer("ballots_layer", delayed.lid),
		)

		f.countVotesFromBallots(
			logger.WithFields(log.Bool("delayed", true)), delayed.lid, delayed.ballots)

		next := front.Next()
		f.delayedQueue.Remove(front)
		front = next
	}
	f.countVotesFromBallots(logger, lid, f.ballots[lid])
}

func (f *full) countVotes(logger log.Log) {
	for lid := f.counted.Add(1); !lid.After(f.processed); lid = lid.Add(1) {
		f.countLayerVotes(logger, lid)
	}
	f.counted = f.processed
}

func (f *full) verify(logger log.Log, lid types.LayerID) bool {
	logger = logger.WithFields(
		log.String("verifier", fullTortoise),
		log.Stringer("counted_layer", f.counted),
		log.Stringer("candidate_layer", lid),
		log.Stringer("local_threshold", f.localThreshold),
		log.Stringer("global_threshold", f.globalThreshold),
	)
	empty, exists := f.empty[lid]
	if !exists {
		return false
	}
	isEmpty := empty.Cmp(f.globalThreshold) > 0
	blocks := f.blocks[lid]
	if len(blocks) == 0 {
		if isEmpty {
			logger.With().Info("candidate layer is empty")
		} else {
			logger.With().Debug("margin is too low to terminate layer as empty",
				lid,
				log.Stringer("margin", empty),
			)
		}
		return isEmpty
	}
	return verifyLayer(
		logger,
		blocks,
		f.validity,
		func(block blockInfo) sign {
			decision := sign(block.weight.Cmp(f.globalThreshold))
			if decision == neutral && isEmpty {
				return against
			}
			return decision
		},
	)
}

// shouldBeDelayed is true if ballot has a different beacon and it wasn't created sufficiently
// long time ago.
func (f *full) shouldBeDelayed(ballotID types.BallotID, ballotlid types.LayerID) bool {
	_, bad := f.badBeaconBallots[ballotID]
	return bad && f.last.Difference(ballotlid) <= f.BadBeaconVoteDelayLayers
}

type delayedBallots struct {
	lid     types.LayerID
	ballots []ballotInfo
}
