package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func newFullTortoise(config Config, state *state) *full {
	return &full{
		Config:  config,
		state:   state,
		delayed: map[types.LayerID][]*ballotInfo{},
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
	// delayed ballots by the layer when they are safe to count
	delayed map[types.LayerID][]*ballotInfo
}

func (f *full) countBallot(logger log.Log, ballot *ballotInfo) {
	if f.shouldBeDelayed(ballot) {
		return
	}
	if ballot.weight.IsNil() {
		return
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
			if bvote.height > ballot.reference.height {
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

func (f *full) countDelayed(logger log.Log, lid types.LayerID) {
	delayed, exist := f.delayed[lid]
	if !exist {
		return
	}
	delete(f.delayed, lid)
	for _, ballot := range delayed {
		f.countBallot(logger, ballot)
	}
}

func (f *full) countVotes(logger log.Log) {
	for lid := f.counted.Add(1); !lid.After(f.processed); lid = lid.Add(1) {
		for _, ballot := range f.ballots[lid] {
			f.countBallot(logger, ballot)
		}
	}
	f.counted = f.processed
}

func (f *full) verify(logger log.Log, lid types.LayerID) bool {
	threshold := f.globalThreshold(f.Config, mode{true, false}, lid)
	logger = logger.WithFields(
		log.String("verifier", fullTortoise),
		log.Stringer("counted_layer", f.counted),
		log.Stringer("candidate_layer", lid),
		log.Stringer("local_threshold", f.localThreshold),
		log.Stringer("global_threshold", threshold),
	)
	layer := f.state.layer(lid)
	empty := layer.empty.Cmp(threshold) > 0
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
			decision := sign(block.margin.Cmp(threshold))
			if decision == neutral && empty {
				return against
			}
			return decision
		},
	)
}

func (f *full) shouldBeDelayed(ballot *ballotInfo) bool {
	if !ballot.conditions.badBeacon {
		return false
	}
	delay := ballot.layer.Add(f.BadBeaconVoteDelayLayers)
	if !delay.After(f.last) {
		return false
	}
	f.delayed[delay] = append(f.delayed[delay], ballot)
	return true
}
