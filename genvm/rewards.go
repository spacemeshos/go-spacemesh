package vm

import (
	"fmt"
	"math"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Config defines the configuration options for Spacemesh rewards.
type Config struct {
	GasLimit          uint64
	StorageCostFactor uint64 `mapstructure:"vm-storage-cost-factor"`
	BaseReward        uint64 `mapstructure:"base-reward"`
}

// DefaultConfig returns the default RewardConfig.
func DefaultConfig() Config {
	return Config{
		GasLimit:          100_000_000,
		StorageCostFactor: 2,
		BaseReward:        50 * uint64(math.Pow10(12)),
	}
}

func calculateLayerReward(cfg Config) uint64 {
	// todo: add inflation rules here
	return cfg.BaseReward
}

// calculateRewards splits layer rewards and total fees fairly between coinbases recorded in rewards.
func calculateRewards(logger log.Log, cfg Config, lid types.LayerID, totalFees uint64, rewards []types.AnyReward) ([]*types.Reward, error) {
	logger = logger.WithFields(lid)
	totalWeight := util.WeightFromUint64(0)
	byCoinbase := make(map[types.Address]util.Weight)
	for _, reward := range rewards {
		weight := util.WeightFromNumDenom(reward.Weight.Num, reward.Weight.Denom)
		logger.With().Debug("coinbase weight", reward.Coinbase, log.Stringer("weight", weight))
		totalWeight.Add(weight)
		if _, ok := byCoinbase[reward.Coinbase]; ok {
			byCoinbase[reward.Coinbase].Add(weight)
		} else {
			byCoinbase[reward.Coinbase] = weight
		}
	}
	if totalWeight.Cmp(util.WeightFromUint64(0)) == 0 {
		logger.Error("zero total weight in block rewards")
		return nil, fmt.Errorf("zero total weight")
	}
	finalRewards := make([]*types.Reward, 0, len(rewards))
	layerRewards := calculateLayerReward(cfg)
	totalRewards := layerRewards + totalFees
	logger.With().Debug("rewards info for layer",
		log.Uint64("layer_rewards", layerRewards),
		log.Uint64("fee", totalFees))
	rewardPer := util.WeightFromUint64(totalRewards).Div(totalWeight)
	lyrRewardPer := util.WeightFromUint64(layerRewards).Div(totalWeight)
	seen := make(map[types.Address]struct{})
	for _, reward := range rewards {
		if _, ok := seen[reward.Coinbase]; ok {
			continue
		}

		seen[reward.Coinbase] = struct{}{}
		weight := byCoinbase[reward.Coinbase]

		fTotal, _ := rewardPer.Copy().Mul(weight).Float64()
		totalReward := uint64(fTotal)
		fLyr, _ := lyrRewardPer.Copy().Mul(weight).Float64()
		finalRewards = append(finalRewards, &types.Reward{
			Layer:       lid,
			Coinbase:    reward.Coinbase,
			TotalReward: totalReward,
			LayerReward: uint64(fLyr),
		})
	}
	return finalRewards, nil
}
