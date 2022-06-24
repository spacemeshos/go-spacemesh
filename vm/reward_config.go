package vm

import (
	"math"
)

// RewardConfig defines the configuration options for Spacemesh rewards.
type RewardConfig struct {
	BaseReward uint64 `mapstructure:"base-reward"`
}

// DefaultRewardConfig returns the default RewardConfig.
func DefaultRewardConfig() RewardConfig {
	return RewardConfig{
		BaseReward: 50 * uint64(math.Pow10(12)),
	}
}

// CalculateLayerReward returns the reward for the given layer.
func (r *RewardConfig) CalculateLayerReward(_ uint32) uint64 {
	// todo: add inflation rules here, this is one of items in the "genesis research pipeline"
	return r.BaseReward
}
