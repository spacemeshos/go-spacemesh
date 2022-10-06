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

func (v *verifying) resetWeights() {
	v.totalGoodWeight = util.WeightFromUint64(0)
	for lid := v.verified.Add(1); !lid.After(v.processed); lid = lid.Add(1) {
		layer := v.layer(lid)
		layer.verifying.good = weight{}
		layer.verifying.abstained = weight{}
	}
}

func (v *verifying) markGoodCut(logger log.Log, ballots []*ballotInfo) bool {
	n := 0
	for _, ballot := range ballots {
		base, exist := v.ballotRefs[ballot.base.id]
		if !exist {
			continue
		}
		if base.canBeGood() && ballot.canBeGood() {
			logger.With().Debug("marking ballots that can be good as good",
				log.Stringer("ballot_layer", ballot.layer),
				log.Stringer("ballot", ballot.id),
				log.Stringer("base_ballot", ballot.base.id),
			)
			base.conditions.baseGood = true
			ballot.conditions.baseGood = true
			n++
		}
	}
	return n > 0
}

func (v *verifying) countVotes(logger log.Log, lid types.LayerID, ballots []*ballotInfo) {
	logger = logger.WithFields(log.Stringer("ballots_layer", lid))
	var (
		sum       = util.WeightFromUint64(0)
		n         int
		refheight = v.maxBaseTickHeight[lid.Sub(1).GetEpoch()]
	)
	for _, ballot := range ballots {
		base, exist := v.ballotRefs[ballot.base.id]
		if !exist {
			continue
		}
		ballot.conditions.baseGood = base.good()
		ballot.conditions.consistent = validateConsistency(v.state, v.Config, ballot)
		logger.With().Debug("ballot goodness",
			ballot.id,
			log.Stringer("base id", ballot.base.id),
			log.Uint32("base layer", ballot.base.layer.Value),
			log.Uint64("max base height", refheight),
			log.Bool("good base", ballot.conditions.baseGood),
			log.Bool("bad beacon", ballot.conditions.badBeacon),
			log.Bool("consistent", ballot.conditions.consistent),
			log.Bool("votes before base", ballot.conditions.votesBeforeBase),
		)
		if !ballot.good() {
			continue
		}
		if ballot.weight.IsNil() {
			logger.With().Debug("ballot weight is nil", ballot.id)
			continue
		}
		// get height of the max votable block
		if refheight > ballot.height {
			logger.With().Debug("reference height is higher than the ballot height",
				ballot.id,
				log.Uint64("reference height", refheight),
				log.Uint64("ballot height", ballot.height),
			)
			continue
		}
		sum = sum.Add(ballot.weight)
		i := uint32(0)
		for current := ballot.votes.tail; current != nil; current = current.prev {
			i++
			if current.vote != abstain {
				continue
			}
			if i > v.Zdist {
				break
			}
			current.verifying.abstained = current.verifying.abstained.Add(ballot.weight)
		}
		n++
	}

	layer := v.layer(lid)
	layer.verifying.good = sum
	v.totalGoodWeight = v.totalGoodWeight.Add(sum)

	logger.With().Debug("counted weight from good ballots",
		log.Stringer("good_weight", sum),
		log.Stringer("total_good_weight", v.totalGoodWeight),
		log.Stringer("expected", v.epochWeight[lid.GetEpoch()]),
		log.Int("ballots_count", len(ballots)),
		log.Int("good_ballots_count", n),
		log.Uint64("layer refheight", refheight),
	)
}

func (v *verifying) verify(logger log.Log, lid types.LayerID) bool {
	// totalGoodWeight      - weight from good ballots in layers [VERIFIED+1, LAST PROCESSED]
	// goodWeight[lid]      - weight from good ballots in the layer that is going to be verified
	// 					      expected to be VERIFIED + 1 (FIXME remove lid parameter)
	// abstainedWeight[lid] - weight from good ballots in layers (VERIFIED+1, LAST PROCESSED]
	//                        that abstained from voting for lid (VERIFIED + 1)
	// TODO(dshulyak) need to know correct values for threshold fractions before implementing next part
	// expectedWeight       - weight from (VERIFIED+1, LAST] layers,
	//                        this is the same weight that is used as a base for global threshold
	// uncountedWeight      - weight that was verifying tortoise didn't count
	//                        expectedWeight - (totalGoodWeight - goodWeight[lid])
	// margin               - this is pessimistic margin that is compared with global threshold
	//                        totalGoodWeight - goodWeight[lid] - abstainedWeight[lid] - uncountedWeight

	refheight := v.maxBaseTickHeight[lid.GetEpoch()]
	layer := v.layer(lid)
	margin := util.WeightFromUint64(0).
		Add(v.totalGoodWeight).
		Sub(layer.verifying.good).
		Sub(layer.verifying.abstained)

	logger = logger.WithFields(
		log.String("verifier", verifyingTortoise),
		log.Stringer("candidate_layer", lid),
		log.Stringer("margin", margin),
		log.Stringer("abstained_weight", layer.verifying.abstained),
		log.Stringer("local_threshold", v.localThreshold),
		log.Stringer("global_threshold", v.globalThreshold),
	)
	if sign(margin.Cmp(v.globalThreshold)) == neutral {
		logger.With().Debug("doesn't cross global threshold")
		return false
	}
	if verifyLayer(
		logger,
		layer.blocks,
		func(block *blockInfo) sign {
			if block.height > refheight {
				return neutral
			}
			decision, _ := getLocalVote(v.state, v.Config, block)
			return decision
		},
	) {
		v.totalGoodWeight.Sub(layer.verifying.good)
		return true
	}
	return false
}
