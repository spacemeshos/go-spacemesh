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
	if f.shouldBeDelayed(logger, ballot) {
		return
	}
	if ballot.weight.IsNil() {
		return
	}
	logger.With().Debug("counted votes from ballot",
		log.Stringer("id", ballot.id),
		log.Uint32("lid", ballot.layer.Value),
	)
	for lvote := ballot.votes.tail; lvote != nil; lvote = lvote.prev {
		if !lvote.lid.After(f.evicted) {
			break
		}
		if lvote.vote == abstain {
			continue
		}
		empty := true
		for _, block := range lvote.blocks {
			if block.height > ballot.reference.height {
				continue
			}
			switch lvote.getVote(block.id) {
			case support:
				empty = false
				block.margin = block.margin.Add(ballot.weight)
			case against:
				block.margin = block.margin.Sub(ballot.weight)
			}
		}
		if empty {
			lvote.empty = lvote.empty.Add(ballot.weight)
		}
	}
}

func (f *full) countForLateBlock(block *blockInfo) {
	// ballots are always added to the state after blocks that are
	// explicitly referenced in the ballot.
	// therefore if block is added later - all previous ballots vote
	// for it negatively.
	//
	// we could store all negative weight in a single variable and avoid
	// this computation if there would be no height
	for lid := block.layer.Add(1); !lid.After(f.counted); lid = lid.Add(1) {
		for _, ballot := range f.ballots[lid] {
			if block.height > ballot.reference.height {
				continue
			}
			block.margin = block.margin.Sub(ballot.weight)
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
		f.countBallot(logger.WithFields(log.Bool("delayed", true)), ballot)
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
	threshold := f.globalThreshold(f.Config, lid)
	logger = logger.WithFields(
		log.String("verifier", "full"),
		log.Stringer("counted_layer", f.counted),
		log.Stringer("candidate_layer", lid),
		log.Stringer("local_threshold", f.localThreshold),
		log.Stringer("global_threshold", threshold),
	)
	layer := f.state.layer(lid)
	empty := layer.empty.Cmp(threshold) > 0
	if len(layer.blocks) == 0 {
		if empty {
			logger.With().Debug("candidate layer is empty")
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

func (f *full) shouldBeDelayed(logger log.Log, ballot *ballotInfo) bool {
	if !ballot.conditions.badBeacon {
		return false
	}
	delay := ballot.layer.Add(f.BadBeaconVoteDelayLayers)
	if !delay.After(f.last) {
		return false
	}
	logger.With().Debug("ballot is delayed",
		log.Stringer("id", ballot.id),
		log.Uint32("ballot lid", ballot.layer.Value),
		log.Uint32("counted at", delay.Value),
	)
	f.delayed[delay] = append(f.delayed[delay], ballot)
	return true
}
