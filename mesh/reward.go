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

func MergeDoubles(transactions []*state.Transaction) []*state.Transaction {
	transactionSet := make(map[common.Hash]struct{})
	merged := make([]*state.Transaction, 0, len(transactions))
	for _, trns := range transactions {
		if _, ok := transactionSet[trns.Hash()]; !ok {
			transactionSet[trns.Hash()] = struct{}{}
			merged = append(merged, trns)
		} else {
			log.Info("double trans merged %v", trns)
		}
	}
	return merged
}

func (m *Mesh) CalculateLayerReward(id LayerID, params RewardParams) *big.Int {
	return params.BaseReward
}

func (m *Mesh) AccumulateRewards(rewardLayer LayerID, params RewardParams) {
	l, err := m.getLayer(rewardLayer)
	if err != nil || l == nil {
		m.Error("") //todo handle error
		return
	}

	ids := make(map[string]struct{})
	uq := make(map[string]struct{})

	//todo: check if block producer was eligible?
	for _, bl := range l.blocks {
		if _, found := ids[bl.MinerID]; found {
			log.Error("two blocks found from same miner %v in layer %v", bl.MinerID, bl.LayerIndex)
			continue
		}
		ids[bl.MinerID] = struct{}{}
		if uint32(len(bl.Txs)) < params.TxQuota {
			//todo: think of giving out reward for unique txs as well
			uq[bl.MinerID] = struct{}{}
		}
	}
	//accumulate all blocks rewards
	txs := m.ExtractOrderedSortedTransactions(l)

	merged := MergeDoubles(txs)
	rewards := &big.Int{}
	processed := 0
	for _, tx := range merged {
		res := new(big.Int).Mul(tx.Price, params.SimpleTxCost)
		processed++
		rewards.Add(rewards, res)
	}

	layerReward := m.CalculateLayerReward(rewardLayer, params)
	rewards.Add(rewards, layerReward)

	numBlocks := big.NewInt(int64(len(l.blocks)))
	log.Info("fees reward: %v total processed %v total txs %v merged %v blocks: %v", rewards.Int64(), processed, len(txs), len(merged), numBlocks)

	bonusReward, diminishedReward := m.calculateActualRewards(rewards, numBlocks, params, len(uq))
	m.state.ApplyRewards(state.LayerID(rewardLayer), ids, uq, bonusReward, diminishedReward)
	//todo: should miner id be sorted in a deterministic order prior to applying rewards?

}

func (m *Mesh) calculateActualRewards(rewards *big.Int, numBlocks *big.Int, params RewardParams, underQuotaBlocks int) (*big.Int, *big.Int) {
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
