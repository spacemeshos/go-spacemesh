package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
)

// computeBallotWeight compute and assign ballot weight to the weights map.
func computeBallotWeight(
	atxdb atxDataProvider,
	bdp blockDataProvider,
	referenceWeights, weights map[types.BallotID]util.Weight,
	ballot *types.Ballot,
	layerSize,
	layersPerEpoch uint32,
) (util.Weight, error) {
	var reference util.Weight
	if ballot.EpochData != nil {
		var total, targetWeight uint64

		for _, atxid := range ballot.EpochData.ActiveSet {
			atx, err := atxdb.GetAtxHeader(atxid)
			if err != nil {
				return util.Weight{}, fmt.Errorf("atx %s in active set of %s is unknown", atxid, ballot.ID())
			}
			atxweight := atx.GetWeight()
			total += atxweight
			if atxid == ballot.AtxID {
				targetWeight = atxweight
			}
		}
		expected, err := proposals.GetNumEligibleSlots(targetWeight, total, layerSize, layersPerEpoch)
		if err != nil {
			return util.Weight{}, fmt.Errorf("unable to compute number of eligibile ballots for atx %s", ballot.AtxID)
		}
		reference = util.WeightFromUint64(targetWeight)
		reference = reference.Div(util.WeightFromUint64(uint64(expected)))
		referenceWeights[ballot.ID()] = reference
	} else {
		if ballot.RefBallot == types.EmptyBallotID {
			return util.Weight{}, fmt.Errorf("empty ref ballot and no epoch data on ballot %s", ballot.ID())
		}
		var exist bool
		reference, exist = referenceWeights[ballot.RefBallot]
		if !exist {
			refballot, err := bdp.GetBallot(ballot.RefBallot)
			if err != nil {
				return util.Weight{}, fmt.Errorf("ref ballot %s for %s is unknown", ballot.ID(), ballot.RefBallot)
			}
			_, err = computeBallotWeight(atxdb, bdp, referenceWeights, weights, refballot, layerSize, layersPerEpoch)
			if err != nil {
				return util.Weight{}, err
			}
			reference = referenceWeights[ballot.RefBallot]
		}
	}
	real := reference.Copy().
		Mul(util.WeightFromInt64(int64(len(ballot.EligibilityProofs))))
	weights[ballot.ID()] = real
	return real, nil
}

func computeEpochWeight(atxdb atxDataProvider, epochWeights map[types.EpochID]util.Weight, eid types.EpochID) (util.Weight, error) {
	layerWeight, exist := epochWeights[eid]
	if exist {
		return layerWeight, nil
	}
	epochWeight, _, err := atxdb.GetEpochWeight(eid)
	if err != nil {
		return util.Weight{}, fmt.Errorf("epoch weight %s: %w", eid, err)
	}
	layerWeight = util.WeightFromUint64(epochWeight)
	layerWeight = layerWeight.Div(util.WeightFromUint64(uint64(types.GetLayersPerEpoch())))
	epochWeights[eid] = layerWeight
	return layerWeight, nil
}

// computes weight for (from, to] layers.
func computeExpectedWeight(weights map[types.EpochID]util.Weight, from, to types.LayerID) util.Weight {
	total := util.WeightFromUint64(0)
	for lid := from.Add(1); !lid.After(to); lid = lid.Add(1) {
		total = total.Add(weights[lid.GetEpoch()])
	}
	return total
}

func computeLocalThreshold(config Config, epochWeight map[types.EpochID]util.Weight, lid types.LayerID) util.Weight {
	threshold := util.WeightFromUint64(0)
	threshold = threshold.Add(epochWeight[lid.GetEpoch()])
	threshold = threshold.Fraction(config.LocalThreshold)
	return threshold
}

func computeThresholdForLayers(config Config, epochWeight map[types.EpochID]util.Weight, target, last types.LayerID) util.Weight {
	expected := computeExpectedWeight(epochWeight, target, last)
	threshold := util.WeightFromUint64(0)
	threshold = threshold.Add(expected)
	threshold = threshold.Fraction(config.GlobalThreshold)
	return threshold
}

func getVerificationWindow(config Config, tmode mode, target, last types.LayerID) types.LayerID {
	if tmode.isFull() && last.Difference(target) > config.FullModeVerificationWindow {
		return target.Add(config.FullModeVerificationWindow)
	} else if tmode.isVerifying() && last.Difference(target) > config.VerifyingModeVerificationWindow {
		return target.Add(config.VerifyingModeVerificationWindow)
	}
	return last
}

func computeThresholds(logger log.Log, config Config, tmode mode,
	target, last, processed types.LayerID,
	epochWeight map[types.EpochID]util.Weight,
) (local, global util.Weight) {
	localThreshold := computeLocalThreshold(config, epochWeight, last)
	globalThreshold := computeThresholdForLayers(config, epochWeight,
		target,
		maxLayer(getVerificationWindow(config, tmode, target, last), processed),
	)
	return localThreshold, globalThreshold.Add(localThreshold)
}
