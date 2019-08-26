package mesh

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"math/big"
)

type Config struct {
	// reward config
	SimpleTxCost   *big.Int
	BaseReward     *big.Int
	PenaltyPercent *big.Int
	TxQuota        uint32
	RewardMaturity types.LayerID
}

func DefaultMeshConfig() Config {
	return Config{
		big.NewInt(10),
		big.NewInt(5000),
		big.NewInt(19),
		15,
		5,
	}
}

func CalculateLayerReward(id types.LayerID, params Config) *big.Int {
	//todo: add inflation rules here
	return params.BaseReward
}

func MergeDoubles(transactions []*Transaction) []*Transaction {
	transactionSet := make(map[types.Hash32]struct{})
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

func calculateActualRewards(rewards *big.Int, numBlocks *big.Int, params Config, underQuotaBlocks int) (*big.Int, *big.Int) {
	log.Info("rewards %v blocks %v penalty_percent %v under_quota %v", rewards.Int64(), numBlocks.Int64(), params.PenaltyPercent.Int64(), underQuotaBlocks)
	mod := new(big.Int)
	// basic_reward =  total rewards / total_num_of_blocks
	blockRewardPerMiner, _ := new(big.Int).DivMod(rewards, numBlocks, mod)
	if mod.Int64() >= numBlocks.Int64()/2 {
		blockRewardPerMiner.Add(blockRewardPerMiner, big.NewInt(1))
	}
	// penalty = basic_reward / 100 * penalty_percent
	rewardPenaltyPerMiner, _ := new(big.Int).DivMod(new(big.Int).Mul(blockRewardPerMiner, params.PenaltyPercent), big.NewInt(100), mod)
	/*if mod.Int64() >= 50 {
		rewardPenaltyPerMiner.Add(rewardPenaltyPerMiner, big.NewInt(1))
	}*/
	// bonus = (penalty * num_of_blocks_with_under_quota_txs) / total_num_of_blocks
	bonusPerMiner, _ := new(big.Int).DivMod(new(big.Int).Mul(rewardPenaltyPerMiner, big.NewInt(int64(underQuotaBlocks))), numBlocks, mod)
	/*if mod.Int64() >= numBlocks.Int64()/2 {
		bonusPerMiner.Add(bonusPerMiner, big.NewInt(1))
	}*/
	// bonus_reward = basic_reward + bonus
	bonusReward := new(big.Int).Add(blockRewardPerMiner, bonusPerMiner)
	// diminished_reward = basic_reward + bonus - penalty
	diminishedReward := new(big.Int).Sub(bonusReward, rewardPenaltyPerMiner)
	log.Info(" rewards  %v blockRewardPerMiner: %v rewardPenaltyPerMiner %v bonusPerMiner %v bonusReward %v diminishedReward %v", rewards, blockRewardPerMiner, rewardPenaltyPerMiner, bonusPerMiner, bonusReward, diminishedReward)
	return bonusReward, diminishedReward
}
