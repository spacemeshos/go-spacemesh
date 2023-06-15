package tortoise

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"go.uber.org/zap"
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

func (f *full) countBallot(logger *zap.Logger, ballot *ballotInfo) {
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
			logger.Debug("counted votes from ballot",
				zap.Stringer("id", ballot.id),
				zap.Uint32("lid", ballot.layer.Uint32()),
				zap.Stringer("block", block.id),
				zap.Stringer("vote", vote),
				zap.Stringer("margin", block.margin),
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

func (f *full) countDelayed(logger *zap.Logger, lid types.LayerID) {
	delayed, exist := f.delayed[lid]
	if !exist {
		return
	}
	delete(f.delayed, lid)
	for _, ballot := range delayed {
		delayedBallots.Dec()
		f.countBallot(logger.With(zap.Bool("delayed", true)), ballot)
	}
}

func (f *full) countVotes(logger *zap.Logger) {
	for lid := f.counted.Add(1); !lid.After(f.processed); lid = lid.Add(1) {
		for _, ballot := range f.ballots[lid] {
			f.countBallot(logger, ballot)
		}
	}
	f.counted = f.processed
}

func (f *full) verify(logger *zap.Logger, lid types.LayerID) bool {
	threshold := f.globalThreshold(f.Config, lid)
	layer := f.state.layer(lid)
	empty := crossesThreshold(layer.empty, threshold) == support

	logger = logger.With(
		zap.String("verifier", "full"),
		zap.Stringer("counted layer", f.counted),
		zap.Stringer("candidate layer", lid),
		zap.Stringer("local threshold", f.localThreshold),
		zap.Stringer("global threshold", threshold),
		zap.Stringer("empty weight", layer.empty),
		zap.Bool("is empty", empty),
	)
	if len(layer.blocks) == 0 {
		if empty {
			logger.Debug("candidate layer is empty")
		} else {
			logger.Debug("margin is too low to terminate layer as empty",
				zap.Uint32("lid", lid.Uint32()),
				zap.Stringer("margin", layer.empty),
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

func (f *full) shouldBeDelayed(logger *zap.Logger, ballot *ballotInfo) bool {
	if !ballot.conditions.badBeacon {
		return false
	}
	delay := ballot.layer.Add(f.BadBeaconVoteDelayLayers)
	if !delay.After(f.last) {
		return false
	}
	logger.Debug("ballot is delayed",
		zap.Stringer("id", ballot.id),
		zap.Uint32("ballot lid", ballot.layer.Uint32()),
		zap.Uint32("counted at", delay.Uint32()),
	)
	delayedBallots.Inc()
	f.delayed[delay] = append(f.delayed[delay], ballot)
	return true
}
