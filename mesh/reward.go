package mesh

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/state"
	"math/big"
)

type RewardParams struct {
	SimpleTxCost   *big.Int
	BaseReward     *big.Int
	PenaltyPercent *big.Int
	TxQuota        uint32
}

//type Transactions []*state.Transaction

func CalculateLayerReward(id LayerID, params RewardParams) *big.Int {
	//todo: add inflation rules here
	return params.BaseReward
}

func MergeDoubles(transactions []*state.Transaction) []*state.Transaction {
	transactionSet := make(map[common.Hash]struct{})
	merged := make([]*state.Transaction, 0, len(transactions))
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

func calculateActualRewards(rewards *big.Int, numBlocks *big.Int, params RewardParams, underQuotaBlocks int) (*big.Int, *big.Int) {
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
