package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

type condition uint8

func (g condition) notConsistent() bool {
	return g == conditionNotConsistent
}

func (g condition) abstained() bool {
	return g&conditionAbstained > 0
}

func (g condition) isCounted() bool {
	return g.isGood() || g.abstained()
}

func (g condition) isGood() bool {
	return g == 0
}

func (g condition) ignored() bool {
	return g&conditionBadBeacon > 0 || g&conditionVotesBeforeBase > 0
}

const (
	conditionBadBeacon condition = 1 << iota
	conditionVotesBeforeBase
	conditionAbstained
	conditionNotConsistent
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
	}
}

func (v *verifying) markGoodCut(logger log.Log, ballots []*ballotInfo) bool {
	n := 0
	for _, ballot := range ballots {
		base, exist := v.ballotRefs[ballot.base.id]
		if !exist {
			continue
		}
		if ballot.goodness.notConsistent() {
			logger.With().Debug("marking ballots that can be good as good",
				log.Stringer("ballot_layer", ballot.layer),
				log.Stringer("ballot", ballot.id),
				log.Stringer("base_ballot", ballot.base.id),
			)
			base.goodness = 0
			ballot.goodness = 0
			n++
		}
	}
	return n > 0
}

func (v *verifying) countVotes(logger log.Log, lid types.LayerID, ballots []*ballotInfo) {
	logger = logger.WithFields(log.Stringer("ballots_layer", lid))
	var (
		sum = util.WeightFromUint64(0)
		n   int
	)
	for _, ballot := range ballots {
		base, exist := v.ballotRefs[ballot.base.id]
		if !exist {
			continue
		}
		if !base.goodness.isGood() {
			continue
		}
		if ballot.goodness.ignored() {
			continue
		}
		if ballot.goodness.notConsistent() {
			ballot.goodness &^= conditionNotConsistent
			if !validateConsistency(v.Config, v.state, ballot) {
				continue
			}
		}
		if ballot.weight.IsNil() {
			logger.With().Debug("ballot weight is nil", ballot.id)
			continue
		}
		// get height of the max votable block
		if refheight := v.layer(lid.Sub(1)).verifying.referenceHeight; refheight > ballot.height {
			logger.With().Debug("reference height is higher than the ballot height",
				ballot.id,
				log.Uint64("reference height", refheight),
				log.Uint64("ballot height", ballot.height),
			)
			continue
		}
		sum = sum.Add(ballot.weight)
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
			if block.height > layer.verifying.referenceHeight {
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

func decodeExceptions(logger log.Log, config Config, state *state, base, ballot *ballotInfo, exceptions *types.Votes) {
	ballot.goodness |= base.goodness
	if _, exist := state.badBeaconBallots[ballot.id]; exist {
		ballot.goodness |= conditionBadBeacon
	}
	// inherit opinion from the base ballot by copying votes in range
	votes := base.votes.Copy()
	// add opinions from the local state [base layer, ballot layer)
	for lid := base.layer; lid.Before(ballot.layer); lid = lid.Add(1) {
		layer := state.layer(lid)
		lvote := layerVote{
			layerInfo: layer,
			vote:      against,
		}
		for _, block := range layer.blocks {
			lvote.blocks = append(lvote.blocks, blockVote{
				blockInfo: block,
				vote:      against,
			})
		}
		votes.Append(&lvote)
	}
	// update exceptions
	ballot.votes = votes
	for _, bid := range exceptions.Support {
		block, exist := state.blockRefs[bid]
		if !exist {
			ballot.goodness |= conditionVotesBeforeBase
			continue
		}
		if block.layer.Before(base.layer) {
			ballot.goodness |= conditionVotesBeforeBase
		}
		ballot.updateBlockVote(block, support)
	}
	for _, bid := range exceptions.Against {
		block, exist := state.blockRefs[bid]
		if !exist {
			ballot.goodness |= conditionVotesBeforeBase
			continue
		}
		if block.layer.Before(base.layer) {
			ballot.goodness |= conditionVotesBeforeBase
		}
		ballot.updateBlockVote(block, against)
	}
	for _, lid := range exceptions.Abstain {
		ballot.goodness |= conditionAbstained
		if lid.Before(base.layer) {
			ballot.goodness |= conditionVotesBeforeBase
		}
		ballot.updateLayerVote(lid, abstain)
		layer := state.layer(lid)
		layer.verifying.abstained = layer.verifying.abstained.Add(ballot.weight)
	}
	validateConsistency(config, state, ballot)
	logger.With().Debug("decoded votes for ballot",
		ballot.id,
		ballot.layer,
		log.Stringer("base", ballot.base.id),
		log.Uint32("base layer", ballot.base.layer.Value),
		log.Bool("base good", base.goodness.isGood()),
		log.Bool("ignored", ballot.goodness.ignored()),
		log.Bool("consistent", !ballot.goodness.notConsistent()),
	)
}

func validateConsistency(config Config, state *state, ballot *ballotInfo) bool {
	for lvote := ballot.votes.Tail; lvote != nil; lvote = lvote.prev {
		if lvote.lid.Before(ballot.base.layer) {
			break
		}
		for j := range lvote.blocks {
			local, _ := getLocalVote(state, config, lvote.blocks[j].blockInfo)
			if vote := lvote.blocks[j].vote; vote != local {
				ballot.goodness |= conditionNotConsistent
				return false
			}
		}
	}
	return true
}
