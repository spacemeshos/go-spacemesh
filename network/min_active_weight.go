package network

import "github.com/spacemeshos/go-spacemesh/common/types"

// MinimalActiveSetWeight is a weight that will replace weight
// recorded in the first ballot, if that weight is less than minimal
// for purposes of eligibility computation.

type GetMinimalActiveSetWeight func(types.EpochID) uint64

func NoopMinimalActiveSetWeight(epoch types.EpochID) uint64 {
	return 0
}

func MainnetMinimalActiveSetWeight(epoch types.EpochID) uint64 {
	if epoch >= 8 {
		// generated using ./cmd/activeset for publish epoch 6
		// it will be used starting from epoch 8, because we will only release it in 7th
		return 7_879_129_244
	}
	return 5_000_000
}

func TestnetMinimalActiveSetWeight(epoch types.EpochID) uint64 {
	return 10_000
}
