package tortoise

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
)

func getBallotHeight(cdb *datastore.CachedDB, ballot *types.Ballot) (uint64, error) {
	atx, err := cdb.GetAtxHeader(ballot.AtxID)
	if err != nil {
		return 0, fmt.Errorf("read atx for ballot height: %w", err)
	}
	return atx.TickHeight(), nil
}

func extractAtxsData(cdb *datastore.CachedDB, epoch types.EpochID) (uint64, uint64, error) {
	var (
		weight  uint64
		heights []uint64
	)
	if err := cdb.IterateEpochATXHeaders(epoch, func(header *types.ActivationTxHeader) bool {
		weight += header.GetWeight()
		heights = append(heights, header.TickHeight())
		return true
	}); err != nil {
		return 0, 0, fmt.Errorf("computing epoch data for %d: %w", epoch, err)
	}
	return weight, getMedian(heights), nil
}

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
		return util.WeightFromUint64(einfo.weight).Fraction(big.NewRat(
			int64(last.Difference(start)+1),
			int64(length),
		))
	}
	weight := util.WeightFromUint64(epochs[startEpoch].weight).Fraction(big.NewRat(
		int64(length-start.OrdinalInEpoch()),
		int64(length),
	))
	for epoch := startEpoch + 1; epoch < lastEpoch; epoch++ {
		einfo := epochs[epoch]
		weight = weight.Add(util.WeightFromUint64(einfo.weight))
	}
	weight = weight.Add(util.WeightFromUint64(epochs[lastEpoch].weight).
		Fraction(big.NewRat(int64(last.OrdinalInEpoch()+1), int64(length))))
	return weight
}

// computeGlobalTreshold computes global treshold based on the expected weight.
func computeGlobalThreshold(config Config, localThreshold weight, epochs map[types.EpochID]*epochInfo, target, processed, last types.LayerID) util.Weight {
	window := last
	if last.Difference(target) > config.WindowSize {
		window = target.Add(config.WindowSize)
	}
	window = maxLayer(window, processed)
	return computeExpectedWeight(epochs,
		target,
		window,
	).
		Fraction(config.GlobalThreshold).
		Add(localThreshold)
}
