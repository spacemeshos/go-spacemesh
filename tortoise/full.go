package tortoise

import (
	"time"

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
	start := time.Now()
	if f.shouldBeDelayed(logger, ballot) {
		return
	}
	if ballot.malicious {
		return
	}
	for lvote := ballot.votes.tail; lvote != nil; lvote = lvote.prev {
		if !lvote.lid.After(f.evicted) {
			break
		}
		if lvote.vote == abstain {
			continue
		}
		layer := f.layer(lvote.lid)
		empty := true
		for _, block := range layer.blocks {
			if block.height > ballot.reference.height {
				continue
			}
			vote := lvote.getVote(block)
			switch vote {
			case support:
				empty = false
				block.margin = block.margin.Add(ballot.weight)
			case against:
				block.margin = block.margin.Sub(ballot.weight)
			}
			logger.With().Debug("counted votes from ballot",
				log.Stringer("id", ballot.id),
				log.Uint32("lid", ballot.layer.Uint32()),
				log.Stringer("block", block.id),
				log.Stringer("vote", vote),
				log.Stringer("margin", block.margin),
			)
		}
		if empty {
			layer.empty = layer.empty.Add(ballot.weight)
		} else {
			layer.empty = layer.empty.Sub(ballot.weight)
		}
	}
	fcountBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
}

func (f *full) countForLateBlock(block *blockInfo) {
	start := time.Now()
	for lid := block.layer.Add(1); !lid.After(f.counted); lid = lid.Add(1) {
		for _, ballot := range f.ballots[lid] {
			if block.height > ballot.reference.height {
				continue
			}
			for current := ballot.votes.tail; current != nil && !current.lid.Before(block.layer); current = current.prev {
				if current.lid != block.layer {
					continue
				}
				if current.vote == abstain {
					continue
				}
				block.margin = block.margin.Sub(ballot.weight)
			}
		}
	}
	lateBlockDuration.Observe(float64(time.Since(start).Nanoseconds()))
}

func (f *full) countDelayed(logger log.Log, lid types.LayerID) {
	delayed, exist := f.delayed[lid]
	if !exist {
		return
	}
	delete(f.delayed, lid)
	for _, ballot := range delayed {
		delayedBallots.Dec()
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
	layer := f.state.layer(lid)
	empty := crossesThreshold(layer.empty, threshold) == support

	logger = logger.WithFields(
		log.String("verifier", "full"),
		log.Stringer("counted layer", f.counted),
		log.Stringer("candidate layer", lid),
		log.Stringer("local threshold", f.localThreshold),
		log.Stringer("global threshold", threshold),
		log.Stringer("empty weight", layer.empty),
		log.Bool("is empty", empty),
	)
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
			decision := crossesThreshold(block.margin, threshold)
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
		log.Uint32("ballot lid", ballot.layer.Uint32()),
		log.Uint32("counted at", delay.Uint32()),
	)
	delayedBallots.Inc()
	f.delayed[delay] = append(f.delayed[delay], ballot)
	return true
}
