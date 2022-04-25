package blocks

import (
	"math"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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

type layerRewardsInfo struct {
	layerID      types.LayerID
	numProposals uint64
	feesReward   uint64
	layerReward  uint64
	// smesher reward per proposal
	totalRewardPer uint64
	// the layer reward part of the smesher reward per proposal
	layerRewardPer uint64
}

// MarshalLogObject implements logging encoder for layerRewardsInfo.
func (info *layerRewardsInfo) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("layer", info.layerID.Value)
	encoder.AddUint64("num_proposals", info.numProposals)
	encoder.AddUint64("total_reward", info.feesReward+info.layerReward)
	encoder.AddUint64("layer_reward", info.layerReward)
	encoder.AddUint64("total_reward_per_proposal", info.totalRewardPer)
	encoder.AddUint64("layer_reward_per_proposal", info.layerRewardPer)
	return nil
}

func calculateLayerReward(cfg RewardConfig) uint64 {
	// todo: add inflation rules here
	return cfg.BaseReward
}

func calculateRewardPerEligibility(layerID types.LayerID, cfg RewardConfig, txs []*types.Transaction, numProposals int) *layerRewardsInfo {
	info := &layerRewardsInfo{layerID: layerID}
	for _, tx := range txs {
		info.feesReward += tx.Fee
	}

	info.layerReward = calculateLayerReward(cfg)
	info.numProposals = uint64(numProposals)

	info.totalRewardPer = (info.feesReward + info.layerReward) / info.numProposals
	info.layerRewardPer = info.layerReward / info.numProposals

	return info
}
