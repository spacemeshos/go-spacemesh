package tortoise

import (
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
		f.countBallot(logger, ballot)
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

func (f *full) verify(logger *zap.Logger, lid types.LayerID) (bool, bool) {
	threshold := f.globalThreshold(f.Config, lid)
	layer := f.state.layer(lid)
	empty := crossesThreshold(layer.empty, threshold) == support

	if len(layer.blocks) == 0 {
		if empty {
			logger.Debug("candidate layer is empty",
				zap.Uint32("candidate layer", lid.Uint32()),
				zap.Float64("global threshold", threshold.Float()),
				zap.Float64("empty weight", layer.empty.Float()),
			)
		} else {
			logger.Debug("margin is too low to terminate layer as empty",
				zap.Uint32("candidate layer", lid.Uint32()),
				zap.Float64("global threshold", threshold.Float()),
				zap.Float64("empty weight", layer.empty.Float()),
			)
		}
		return empty, false
	}
	logger.Debug("global treshold",
		zap.Uint32("target", lid.Uint32()),
		zap.Float64("threshold", threshold.Float()),
	)
	rst, changes := verifyLayer(
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
	if changes {
		logger.Info("candidate layer is verified",
			zapBlocks(layer.blocks),
			zap.String("verifier", "full"),
			zap.Uint32("counted layer", f.counted.Uint32()),
			zap.Uint32("candidate layer", lid.Uint32()),
			zap.Float64("local threshold", f.localThreshold.Float()),
			zap.Float64("global threshold", threshold.Float()),
			zap.Float64("empty weight", layer.empty.Float()),
			zap.Bool("is empty", empty),
		)
	}
	return rst, changes
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
