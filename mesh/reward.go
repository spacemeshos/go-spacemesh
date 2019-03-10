package mesh

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"math/big"
)

type RewardConfig struct {
	SimpleTxCost   *big.Int
	BaseReward     *big.Int
	PenaltyPercent *big.Int
	TxQuota        uint32
	RewardMaturity LayerID
}

func DefaultRewardConfig() RewardConfig {
	return RewardConfig{
		big.NewInt(10),
		big.NewInt(5000),
		big.NewInt(15),
		15,
		5,
	}
}

func CalculateLayerReward(id LayerID, params RewardConfig) *big.Int {
	//todo: add inflation rules here
	return params.BaseReward
}

func MergeDoubles(transactions []*Transaction) []*Transaction {
	transactionSet := make(map[common.Hash]struct{})
	merged := make([]*Transaction, 0, len(transactions))
	for _, trns := range transactions {
		if _, ok := transactionSet[trns.Hash()]; !ok {
			transactionSet[trns.Hash()] = struct{}{}
			merged = append(merged, trns)
		} else {
			log.Debug("double trans merged %v", trns)
		}
	}
	return merged
}

func calculateActualRewards(rewards *big.Int, numBlocks *big.Int, params RewardConfig, underQuotaBlocks int) (*big.Int, *big.Int) {
	mod := new(big.Int)
	// basic_reward =  total rewards / total_num_of_blocks
	blockRewardPerMiner, _ := new(big.Int).DivMod(rewards, numBlocks, mod)
	if mod.Int64() >= numBlocks.Int64()/2 {
		blockRewardPerMiner.Add(blockRewardPerMiner, big.NewInt(1))
	}
	// penalty = basic_reward / 100 * penalty_percent
	rewardPenaltyPerMiner := new(big.Int).Mul(new(big.Int).Div(rewards, big.NewInt(100)), params.PenaltyPercent)
	// bonus = (penalty * num_of_blocks_with_under_quota_txs) / total_num_of_blocks
	bonusPerMiner := new(big.Int).Div(new(big.Int).Mul(rewardPenaltyPerMiner, big.NewInt(int64(underQuotaBlocks))), numBlocks)
	// bonus_reward = basic_reward + bonus
	bonusReward := new(big.Int).Add(blockRewardPerMiner, bonusPerMiner)
	// diminished_reward = basic_reward - penalty
	diminishedReward := new(big.Int).Sub(bonusReward, rewardPenaltyPerMiner)
	log.Info(" rewards  %v blockRewardPerMiner: %v rewardPenaltyPerMiner %v bonusPerMiner %v bonusReward %v diminishedReward %v", rewards, blockRewardPerMiner, rewardPenaltyPerMiner, bonusPerMiner, bonusReward, diminishedReward)
	return bonusReward, diminishedReward
}
