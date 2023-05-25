package tortoise

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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

func (v *verifying) countBallot(logger log.Log, ballot *ballotInfo) {
	start := time.Now()

	prev := v.layer(ballot.layer.Sub(1))
	counted := !(ballot.conditions.badBeacon ||
		prev.opinion != ballot.opinion() ||
		prev.verifying.referenceHeight > ballot.reference.height)
	logger.With().Debug("count ballot in verifying mode",
		ballot.layer,
		ballot.id,
		log.ShortStringer("ballot opinion", ballot.opinion()),
		log.ShortStringer("local opinion", prev.opinion),
		log.Bool("bad beacon", ballot.conditions.badBeacon),
		log.Stringer("weight", ballot.weight),
		log.Uint64("reference height", prev.verifying.referenceHeight),
		log.Uint64("ballot height", ballot.reference.height),
		log.Bool("counted", counted),
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

func (v *verifying) countVotes(logger log.Log, ballots []*ballotInfo) {
	for _, ballot := range ballots {
		v.countBallot(logger, ballot)
	}
}

func (v *verifying) verify(logger log.Log, lid types.LayerID) bool {
	layer := v.layer(lid)
	if !layer.hareTerminated {
		logger.With().Debug("hare is not terminated")
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
	logger = logger.WithFields(
		log.String("verifier", "verifying"),
		log.Stringer("candidate layer", lid),
		log.Stringer("margin", margin),
		log.Stringer("uncounted", uncounted),
		log.Stringer("total good weight", v.totalGoodWeight),
		log.Stringer("good uncounted", layer.verifying.goodUncounted),
		log.Stringer("global threshold", threshold),
	)
	if crossesThreshold(margin, threshold) != support {
		logger.With().Debug("doesn't cross global threshold")
		return false
	} else {
		logger.With().Debug("crosses global threshold")
	}
	if len(layer.blocks) == 0 {
		logger.With().Debug("candidate layer is empty")
	}
	return verifyLayer(
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
}
