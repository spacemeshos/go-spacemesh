package tortoise

import (
	"container/list"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func newFullTortoise(config Config, state *state) *full {
	return &full{
		Config:       config,
		state:        state,
		delayedQueue: list.New(),
	}
}

type full struct {
	Config
	*state

	// counted weights up to this layer.
	//
	// counting votes is what makes full tortoise expensive during rerun.
	// we want to wait until verifying can't make progress before they are counted.
	// storing them in current version is cheap.
	counted types.LayerID
	// queue of the ballots with bad beacon
	delayedQueue *list.List
}

func (f *full) countVotesFromBallots(logger log.Log, ballots []*ballotInfo) {
	var delayed []*ballotInfo
	for _, ballot := range ballots {
		if f.shouldBeDelayed(ballot) {
			delayed = append(delayed, ballot)
			continue
		}
		if ballot.weight.IsNil() {
			continue
		}
		for lvote := ballot.votes.tail; lvote != nil; lvote = lvote.prev {
			if !lvote.lid.After(f.verified) {
				break
			}
			if lvote.vote == abstain {
				continue
			}
			empty := true
			for _, bvote := range lvote.blocks {
				if bvote.height > ballot.height {
					continue
				}
				switch bvote.vote {
				case support:
					empty = false
					bvote.margin = bvote.margin.Add(ballot.weight)
				case against:
					bvote.margin = bvote.margin.Sub(ballot.weight)
				}
			}
			if empty {
				lvote.empty = lvote.empty.Add(ballot.weight)
			}
		}
	}
	if len(delayed) > 0 {
		f.delayedQueue.PushBack(delayedBallots{
			lid:     delayed[0].layer,
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
			logger.WithFields(log.Bool("delayed", true)), delayed.ballots)

		next := front.Next()
		f.delayedQueue.Remove(front)
		front = next
	}
	f.countVotesFromBallots(logger, f.ballots[lid])
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
	layer := f.state.layer(lid)
	empty := layer.empty.Cmp(f.globalThreshold) > 0
	if len(layer.blocks) == 0 {
		if empty {
			logger.With().Info("candidate layer is empty")
		} else {
			logger.With().Debug("margin is too low to terminate layer as empty",
				lid,
				log.Stringer("margin", layer.empty),
			)
		}
		return empty
	}
	return verifyLayer(
		logger,
		layer.blocks,
		func(block *blockInfo) sign {
			decision := sign(block.margin.Cmp(f.globalThreshold))
			if decision == neutral && empty {
				return against
			}
			return decision
		},
	)
}

// shouldBeDelayed is true if ballot has a different beacon and it wasn't created sufficiently
// long time ago.
func (f *full) shouldBeDelayed(ballot *ballotInfo) bool {
	_, bad := f.badBeaconBallots[ballot.id]
	return bad && f.last.Difference(ballot.layer) <= f.BadBeaconVoteDelayLayers
}

type delayedBallots struct {
	lid     types.LayerID
	ballots []*ballotInfo
}
