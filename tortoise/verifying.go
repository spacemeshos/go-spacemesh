package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

type condition uint8

func (g condition) notConsistent() bool {
	return g&conditionNotConsistent > 0
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

func (v *verifying) markGoodCut(logger log.Log, ballots []*ballotInfoV2) bool {
	n := 0
	for _, ballot := range ballots {
		base, exist := v.ballotRefs[ballot.base.id]
		if !exist {
			continue
		}
		if ballot.goodness.isCounted() {
			logger.With().Debug("marking ballots that can be good as good",
				log.Stringer("ballot_layer", ballot.layer),
				log.Stringer("ballot", ballot.id),
				log.Stringer("base_ballot", ballot.base.id),
			)
			base.goodness = 0
			n++
		}
	}
	return n > 0
}

func (v *verifying) countVotes(logger log.Log, lid types.LayerID, ballots []*ballotInfoV2) {
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
		ballot.goodness &= ^conditionNotConsistent
		if !(base.goodness.isGood() && ballot.goodness.isCounted()) {
			continue
		}
		if !validateConsistency(v.Config, v.state, ballot) {
			continue
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
		func(block *blockInfoV2) sign {
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

func decodeExceptions(logger log.Log, config Config, state *state, base, child *ballotInfoV2, exceptions *types.Votes) {
	child.goodness |= base.goodness
	if _, exist := state.badBeaconBallots[child.id]; exist {
		child.goodness |= conditionBadBeacon
	}
	// inherit opinion from the base ballot by copying votes in range (evicted layer, base layer)
	votes := base.copyVotes(state.evicted)
	// add opinions from the local state [base layer, ballot layer)
	for lid := base.layer; lid.Before(child.layer); lid = lid.Add(1) {
		layer := state.layer(lid)
		lvote := layerVote{
			layer: layer,
			vote:  against,
		}
		for _, block := range layer.blocks {
			lvote.blocks = append(lvote.blocks, blockVote{
				block: block,
				vote:  against,
			})
		}
		votes = append(votes, lvote)
	}
	// update exceptions
	child.votes = votes
	for _, bid := range exceptions.Support {
		block, exist := state.blockRefs[bid]
		if !exist {
			child.goodness |= conditionVotesBeforeBase
			continue
		}
		if block.layer.Before(base.layer) {
			child.goodness |= conditionVotesBeforeBase
		}
		child.updateBlockVote(block, support)
	}
	for _, bid := range exceptions.Against {
		block, exist := state.blockRefs[bid]
		if !exist {
			child.goodness |= conditionVotesBeforeBase
			continue
		}
		if block.layer.Before(base.layer) {
			child.goodness |= conditionVotesBeforeBase
		}
		child.updateBlockVote(block, against)
	}
	for _, lid := range exceptions.Abstain {
		child.goodness |= conditionAbstained
		if lid.Before(base.layer) {
			child.goodness |= conditionVotesBeforeBase
		}
		child.updateLayerVote(lid, abstain)
		layer := state.layer(lid)
		layer.verifying.abstained = layer.verifying.abstained.Add(child.weight)
	}
	validateConsistency(config, state, child)
	logger.With().Debug("decoded votes for ballot",
		child.id,
		child.layer,
		log.Stringer("base", child.base.id),
		log.Uint32("base layer", child.base.layer.Value),
		log.Bool("base good", base.goodness.isGood()),
		log.Bool("counted", child.goodness.isCounted()),
	)
}

func validateConsistency(config Config, state *state, ballot *ballotInfoV2) bool {
	for i := len(ballot.votes) - 1; i >= 0; i-- {
		lvote := ballot.votes[i]
		if lvote.layer.lid.Before(ballot.base.layer) {
			return true
		}
		for j := range lvote.blocks {
			local, _ := getLocalVote(state, config, lvote.blocks[j].block)
			if vote := lvote.blocks[j].vote; vote != local {
				ballot.goodness |= conditionNotConsistent
				return false
			}
		}
	}
	return true
}
