package minweight

import "github.com/spacemeshos/go-spacemesh/common/types"

func Select(epoch types.EpochID, weights []types.EpochMinimalActiveWeight) uint64 {
	var (
		rst  uint64
		prev types.EpochID
	)
	for _, weight := range weights {
		if weight.Epoch < prev {
			panic("weights are not sorted by epoch")
		}
		if epoch >= weight.Epoch {
			rst = weight.Weight
		}
		prev = weight.Epoch
	}
	return rst
}
