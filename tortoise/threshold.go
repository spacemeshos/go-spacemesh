package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
)

// computeBallotWeight compute and assign ballot weight to the weights map.
func computeBallotWeight(
	atxdb atxDataProvider,
	bdp blockDataProvider,
	weights map[types.BallotID]weight,
	ballot *types.Ballot,
	layerSize,
	layersPerEpoch uint32,
) (weight, error) {
	if ballot.EpochData != nil {
		var total, targetWeight uint64

		for _, atxid := range ballot.EpochData.ActiveSet {
			atx, err := atxdb.GetAtxHeader(atxid)
			if err != nil {
				return weight{}, fmt.Errorf("atx %s in active set of %s is unknown", atxid, ballot.ID())
			}
			atxweight := atx.GetWeight()
			total += atxweight
			if atxid == ballot.AtxID {
				targetWeight = atxweight
			}
		}

		expected, err := proposals.GetNumEligibleSlots(targetWeight, total, layerSize, layersPerEpoch)
		if err != nil {
			return weight{}, fmt.Errorf("unable to compute number of eligibile ballots for atx %s", ballot.AtxID)
		}
		rst := weightFromUint64(targetWeight)
		rst = rst.div(weightFromUint64(uint64(expected)))
		weights[ballot.ID()] = rst
		return rst, nil
	}
	if ballot.RefBallot == types.EmptyBallotID {
		return weight{}, fmt.Errorf("empty ref ballot and no epoch data on ballot %s", ballot.ID())
	}
	rst, exist := weights[ballot.RefBallot]
	if !exist {
		refballot, err := bdp.GetBallot(ballot.RefBallot)
		if err != nil {
			return weight{}, fmt.Errorf("ref ballot %s for %s is unknown", ballot.ID(), ballot.RefBallot)
		}
		rst, err = computeBallotWeight(atxdb, bdp, weights, refballot, layerSize, layersPerEpoch)
		if err != nil {
			return weight{}, err
		}
	}
	weights[ballot.ID()] = rst
	return rst, nil
}

func computeEpochWeight(atxdb atxDataProvider, epochWeights map[types.EpochID]weight, eid types.EpochID) (weight, error) {
	layerWeight, exist := epochWeights[eid]
	if exist {
		return layerWeight, nil
	}
	epochWeight, _, err := atxdb.GetEpochWeight(eid)
	if err != nil {
		return weight{}, fmt.Errorf("epoch weight %s: %w", eid, err)
	}
	layerWeight = weightFromUint64(epochWeight)
	layerWeight = layerWeight.div(weightFromUint64(uint64(types.GetLayersPerEpoch())))
	epochWeights[eid] = layerWeight
	return layerWeight, nil
}

// computes weight for (from, to] layers.
func computeExpectedWeight(weights map[types.EpochID]weight, from, to types.LayerID) weight {
	total := weightFromUint64(0)
	for lid := from.Add(1); !lid.After(to); lid = lid.Add(1) {
		total = total.add(weights[lid.GetEpoch()])
	}
	return total
}

func computeLocalThreshold(config Config, epochWeight map[types.EpochID]weight, lid types.LayerID) weight {
	threshold := weightFromUint64(0)
	threshold = threshold.add(epochWeight[lid.GetEpoch()])
	threshold = threshold.fraction(config.LocalThreshold)
	return threshold
}

func computeThresholdForLayers(config Config, epochWeight map[types.EpochID]weight, target, last types.LayerID) weight {
	expected := computeExpectedWeight(epochWeight, target, last)
	threshold := weightFromUint64(0)
	threshold = threshold.add(expected)
	threshold = threshold.fraction(config.GlobalThreshold)
	return threshold
}

func getVerificationWindow(config Config, state *commonState, tmode mode) types.LayerID {
	target := state.verified.Add(1)
	window := state.last
	if tmode.isFull() && state.last.Difference(target) > config.FullModeVerificationWindow {
		window = target.Add(config.FullModeVerificationWindow)
	} else if tmode.isVerifying() && state.last.Difference(target) > config.VerifyingModeVerificationWindow {
		window = target.Add(config.VerifyingModeVerificationWindow)
	}
	return window
}

// updateThresholds recomputes local and global thresholds:
// - when last layer is updated
// - when verified layer is updated
// - when switching from one mode into the other.
func updateThresholds(logger log.Log, config Config, state *commonState, tmode mode) {
	state.localThreshold = computeLocalThreshold(config, state.epochWeight, state.last)

	window := maxLayer(getVerificationWindow(config, state, tmode), state.processed)

	target := state.verified.Add(1)
	state.globalThreshold = computeThresholdForLayers(config, state.epochWeight, target, window)
	state.globalThreshold = state.globalThreshold.add(state.localThreshold)

	logger.With().Info("updated thresholds",
		log.Stringer("window", window),
		log.Stringer("last_layer", state.last),
		log.Stringer("processed_layer", state.processed),
		log.Stringer("target_layer", target),
		log.Stringer("local_threshold", state.localThreshold),
		log.Stringer("global_threshold", state.globalThreshold),
		log.Stringer("mode", tmode),
	)
}
