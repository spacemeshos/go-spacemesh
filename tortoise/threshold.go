package tortoise

import (
	"sort"

	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	// by assumption adversarial weight can't be larger than 1/3.
	adversarialWeightFraction = 3
	// nodes should not be on different sides of the local threshold if they receive different adversarial votes.
	localThresholdFraction = 3
	// for comleteness:
	// global threshold is set in a such way so that if adversary
	// cancels their weight (adversarialWeightFraction) - honest nodes should still cross local threshold.
)

func getMedian(heights []uint64) uint64 {
	if len(heights) == 0 {
		return 0
	}
	sort.Slice(heights, func(i, j int) bool {
		return heights[i] < heights[j]
	})
	mid := len(heights) / 2
	if len(heights)%2 == 0 {
		return (heights[mid-1] + heights[mid]) / 2
	}
	return heights[mid]
}

func computeExpectedWeight(epochs map[types.EpochID]*epochInfo, target, last types.LayerID) weight {
	// all layers after target are voting on the target layer
	// therefore expected weight for the target layer is a sum of all weights
	// within (target, last]
	start := target.Add(1)
	startEpoch := start.GetEpoch()
	lastEpoch := last.GetEpoch()
	length := types.GetLayersPerEpoch()
	if startEpoch == lastEpoch {
		einfo := epochs[startEpoch]
		return einfo.weight.
			Mul(fixed.New64(int64(last.Difference(start)) + 1)).
			Div(fixed.New64(int64(length)))
	}
	weight := epochs[startEpoch].weight.
		Mul(fixed.New64(int64(length - start.OrdinalInEpoch()))).
		Div(fixed.New64(int64(length)))
	for epoch := startEpoch + 1; epoch < lastEpoch; epoch++ {
		einfo := epochs[epoch]
		weight = weight.Add(einfo.weight)
	}
	return weight.Add(epochs[lastEpoch].weight.
		Mul(fixed.New64(int64(last.OrdinalInEpoch() + 1))).
		Div(fixed.New64(int64(length))),
	)
}

// computeGlobalThreshold computes global threshold based on the expected weight.
func computeGlobalThreshold(
	config Config,
	localThreshold weight,
	epochs map[types.EpochID]*epochInfo,
	target, processed, last types.LayerID,
) weight {
	return computeExpectedWeightInWindow(config, epochs, target, processed, last).
		Div(fixed.New(adversarialWeightFraction)).
		Add(localThreshold)
}

func computeExpectedWeightInWindow(
	config Config,
	epochs map[types.EpochID]*epochInfo,
	target, processed, last types.LayerID,
) weight {
	window := last
	windowSize := config.WindowSizeLayers(target)
	if last.Difference(target) > windowSize.Uint32() {
		window = target.Add(windowSize.Uint32())
		if processed.After(window) {
			window = processed
		}
	}
	return computeExpectedWeight(epochs, target, window)
}

func crossesThreshold(w, t weight) sign {
	if w.Float() > 0 && w.GreaterThan(t) {
		return support
	}
	if w.Float() < 0 && w.Abs().GreaterThan(t) {
		return against
	}
	return neutral
}
