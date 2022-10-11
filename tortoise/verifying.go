package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
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
	totalGoodWeight util.Weight
}

// reset all weight that can vote on a voted layer
func (v *verifying) resetWeights(voted types.LayerID) {
	vlayer := v.layer(voted)
	v.totalGoodWeight = vlayer.verifying.goodUncounted.Copy()
	for lid := voted.Add(1); !lid.After(v.processed); lid = lid.Add(1) {
		layer := v.layer(lid)
		layer.verifying.goodUncounted = vlayer.verifying.goodUncounted.Copy()
	}
}

func (v *verifying) countBallot(logger log.Log, ballot *ballotInfo) {
	base, exist := v.ballotRefs[ballot.base.id]
	if !exist {
		return
	}
	ballot.conditions.baseGood = base.good()
	ballot.conditions.consistent = validateConsistency(v.state, v.Config, ballot)
	logger.With().Debug("ballot goodness",
		ballot.id,
		ballot.layer,
		log.Stringer("base id", ballot.base.id),
		log.Uint32("base layer", ballot.base.layer.Value),
		log.Bool("good", ballot.good()))
	if !ballot.good() {
		return
	}
	if ballot.weight.IsNil() {
		logger.With().Debug("ballot weight is nil", ballot.id)
		return
	}
	// get height of the max votable block
	if refheight := v.layer(ballot.layer.Sub(1)).verifying.referenceHeight; refheight > ballot.reference.height {
		logger.With().Debug("reference height is higher than the ballot height",
			ballot.id,
			log.Uint64("reference height", refheight),
			log.Uint64("ballot height", ballot.reference.height),
		)
		return
	}
	for lid := ballot.layer; !lid.After(v.processed); lid = lid.Add(1) {
		layer := v.layer(lid)
		layer.verifying.goodUncounted = layer.verifying.goodUncounted.Add(ballot.weight)
	}
	v.totalGoodWeight = v.totalGoodWeight.Add(ballot.weight)
	v.countAbstained(ballot)
}

func (v *verifying) countAbstained(ballot *ballotInfo) {
	i := uint32(0)
	for current := ballot.votes.tail; current != nil; current = current.prev {
		i++
		if current.vote != abstain {
			continue
		}
		if i > v.Zdist {
			return
		}
		current.verifying.abstained = current.verifying.abstained.Add(ballot.weight)
	}
}

func (v *verifying) countVotes(logger log.Log, ballots []*ballotInfo) {
	for _, ballot := range ballots {
		v.countBallot(logger, ballot)
	}
}

func (v *verifying) verify(logger log.Log, lid types.LayerID) bool {
	layer := v.layer(lid)
	margin := util.WeightFromUint64(0).
		Add(v.totalGoodWeight).
		Sub(layer.verifying.goodUncounted).
		Sub(layer.verifying.abstained)

	threshold := v.globalThreshold(v.Config, lid)
	logger = logger.WithFields(
		log.String("verifier", "verifying"),
		log.Stringer("candidate_layer", lid),
		log.Stringer("margin", margin),
		log.Stringer("abstained_weight", layer.verifying.abstained),
		log.Stringer("local_threshold", v.localThreshold),
		log.Stringer("global_threshold", threshold),
	)

	if sign(margin.Cmp(threshold)) == neutral {
		logger.With().Debug("doesn't cross global threshold")
		return false
	}
	return verifyLayer(
		logger,
		layer.blocks,
		func(block *blockInfo) sign {
			if block.height > layer.verifying.referenceHeight {
				return neutral
			}
			decision, _ := getLocalVote(v.state, v.Config, block)
			return decision
		},
	)
}
