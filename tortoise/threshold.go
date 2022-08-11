package tortoise

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
)

// computeBallotWeight compute ballot weight.
func computeBallotWeight(
	cdb *datastore.CachedDB,
	referenceWeights map[types.BallotID]util.Weight,
	ballot *types.Ballot,
	layerSize,
	layersPerEpoch uint32,
) (util.Weight, error) {
	ref := ballot.ID()
	if ballot.EpochData == nil {
		if ballot.RefBallot == types.EmptyBallotID {
			return util.Weight{}, fmt.Errorf("empty ref ballot and no epoch data on ballot %s", ballot.ID())
		}
		ref = ballot.RefBallot
	}
	var (
		reference util.Weight
		err       error
		exist     bool
	)
	if reference, exist = referenceWeights[ref]; !exist {
		reference, err = proposals.ComputeWeightPerEligibility(cdb, ballot, layerSize, layersPerEpoch)
		if err != nil {
			return util.Weight{}, fmt.Errorf("get ballot weight %w", err)
		}
		referenceWeights[ref] = reference
	}
	return reference.Copy().Mul(util.WeightFromInt64(int64(len(ballot.EligibilityProofs)))), nil
}

func computeEpochWeight(cdb *datastore.CachedDB, epochWeights map[types.EpochID]util.Weight, eid types.EpochID) (util.Weight, error) {
	layerWeight, exist := epochWeights[eid]
	if exist {
		return layerWeight, nil
	}
	epochWeight, _, err := cdb.GetEpochWeight(eid)
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
