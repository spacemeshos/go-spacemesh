package tortoise

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"go.uber.org/zap"
)

func newVerifying(config Config, state *state) *verifying {
	return &verifying{
		Config: config,
		state:  state,
	}
}

type verifying struct {
	Config
	*state

	// total weight of good ballots from verified + 1 up to last processed
	totalGoodWeight weight
}

// reset all weight that can vote on a voted layer.
func (v *verifying) resetWeights(voted types.LayerID) {
	vlayer := v.layer(voted)
	v.totalGoodWeight = vlayer.verifying.goodUncounted
	for lid := voted.Add(1); !lid.After(v.processed); lid = lid.Add(1) {
		layer := v.layer(lid)
		layer.verifying.goodUncounted = vlayer.verifying.goodUncounted
	}
}

func (v *verifying) countBallot(logger *zap.Logger, ballot *ballotInfo) {
	start := time.Now()

	prev := v.layer(ballot.layer.Sub(1))
	counted := !(ballot.conditions.badBeacon ||
		prev.opinion != ballot.opinion() ||
		prev.verifying.referenceHeight > ballot.reference.height)
	logger.Debug("count ballot in verifying mode",
		zap.Uint32("lid", ballot.layer.Uint32()),
		zap.Stringer("ballot", ballot.id),
		log.ZShortStringer("ballot opinion", ballot.opinion()),
		log.ZShortStringer("local opinion", prev.opinion),
		zap.Bool("bad beacon", ballot.conditions.badBeacon),
		zap.Float64("weight", ballot.weight.Float()),
		zap.Uint64("reference height", prev.verifying.referenceHeight),
		zap.Uint64("ballot height", ballot.reference.height),
		zap.Bool("counted", counted),
	)
	if !counted {
		return
	}

	for lid := ballot.layer; !lid.After(v.processed); lid = lid.Add(1) {
		layer := v.layer(lid)
		layer.verifying.goodUncounted = layer.verifying.goodUncounted.Add(ballot.weight)
	}
	v.totalGoodWeight = v.totalGoodWeight.Add(ballot.weight)
	vcountBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
}

func (v *verifying) countVotes(logger *zap.Logger, ballots []*ballotInfo) {
	for _, ballot := range ballots {
		v.countBallot(logger, ballot)
	}
}

func (v *verifying) verify(logger *zap.Logger, lid types.LayerID) bool {
	layer := v.layer(lid)
	if !layer.hareTerminated {
		logger.Debug("hare is not terminated")
		return false
	}

	margin := v.totalGoodWeight.
		Sub(layer.verifying.goodUncounted)
	uncounted := v.expectedWeight(v.Config, lid).
		Sub(margin)
	// GreaterThan(zero) returns true even if value with negative sign
	if uncounted.Float() > 0 {
		margin = margin.Sub(uncounted)
	}

	threshold := v.globalThreshold(v.Config, lid)
	if crossesThreshold(margin, threshold) != support {
		logger.Debug("doesn't cross global threshold",
			zap.Uint32("candidate layer", lid.Uint32()),
			zap.Float64("margin", margin.Float()),
			zap.Float64("global threshold", threshold.Float()),
		)
		return false
	} else {
		logger.Debug("crosses global threshold",
			zap.Uint32("candidate layer", lid.Uint32()),
			zap.Float64("margin", margin.Float()),
			zap.Float64("global threshold", threshold.Float()),
		)
	}
	if len(layer.blocks) == 0 {
		logger.Debug("candidate layer is empty",
			zap.Uint32("candidate layer", lid.Uint32()),
			zap.Float64("margin", margin.Float()),
			zap.Float64("global threshold", threshold.Float()),
		)
	}
	rst, changes := verifyLayer(
		logger,
		layer.blocks,
		func(block *blockInfo) sign {
			if block.height > layer.verifying.referenceHeight {
				return neutral
			}
			decision, _ := getLocalVote(v.Config, v.state.verified, v.state.last, block)
			return decision
		},
	)
	if changes {
		logger.Info("candidate layer is verified",
			zapBlocks(layer.blocks),
			zap.String("verifier", "verifying"),
			zap.Uint32("candidate layer", lid.Uint32()),
			zap.Float64("margin", margin.Float()),
			zap.Float64("global threshold", threshold.Float()),
			zap.Float64("uncounted", uncounted.Float()),
			zap.Float64("total good weight", v.totalGoodWeight.Float()),
			zap.Float64("good uncounted", layer.verifying.goodUncounted.Float()),
		)
	}
	return rst
}
