package vm

import (
	"fmt"
	"math/big"

	"github.com/spacemeshos/economics/rewards"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/athenavm/core"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

func (v *VM) addRewards(
	layer types.LayerID,
	ss *core.StagedCache,
	fees uint64,
	blockRewards []types.CoinbaseReward,
) ([]types.Reward, error) {
	var (
		layersAfterEffectiveGenesis = layer.Difference(types.FirstEffectiveGenesis())
		subsidy                     = rewards.TotalSubsidyAtLayer(layersAfterEffectiveGenesis)
		total                       = subsidy + fees
		transferred                 uint64
		totalWeight                 = new(big.Rat)
	)
	for _, blockReward := range blockRewards {
		totalWeight.Add(totalWeight, blockReward.Weight.ToBigRat())
	}
	result := make([]types.Reward, 0, len(blockRewards))
	for _, blockReward := range blockRewards {
		relative := blockReward.Weight.ToBigRat()
		relative.Quo(relative, totalWeight)

		totalReward := new(big.Int).SetUint64(total)
		totalReward.
			Mul(totalReward, relative.Num()).
			Quo(totalReward, relative.Denom())
		if !totalReward.IsUint64() {
			return nil, fmt.Errorf("%w: total reward %v for %v overflows uint64",
				core.ErrInternal, totalReward, blockReward.Coinbase)
		}

		subsidyReward := new(big.Int).SetUint64(subsidy)
		subsidyReward.
			Mul(subsidyReward, relative.Num()).
			Quo(subsidyReward, relative.Denom())
		if !subsidyReward.IsUint64() {
			return nil, fmt.Errorf("%w: subsidy reward %v for %v overflows uint64",
				core.ErrInternal, subsidyReward, blockReward.Coinbase)
		}

		v.logger.Debug("rewards for coinbase",
			zap.Uint32("layer", layer.Uint32()),
			zap.Stringer("coinbase", blockReward.Coinbase),
			zap.Stringer("relative weight", &blockReward.Weight),
			zap.Uint64("subsidy", subsidyReward.Uint64()),
			zap.Uint64("total", totalReward.Uint64()),
		)

		reward := types.Reward{
			Layer:       layer,
			Coinbase:    blockReward.Coinbase,
			SmesherID:   blockReward.SmesherID,
			TotalReward: totalReward.Uint64(),
			LayerReward: subsidyReward.Uint64(),
		}
		result = append(result, reward)
		account, err := ss.Get(blockReward.Coinbase)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", core.ErrInternal, err)
		}
		account.Balance += reward.TotalReward
		if err := ss.Update(account); err != nil {
			return nil, fmt.Errorf("%w: %w", core.ErrInternal, err)
		}
		transferred += totalReward.Uint64()
	}
	v.logger.Debug("rewards for layer",
		zap.Uint32("layer", layer.Uint32()),
		zap.Uint32("after genesis", layersAfterEffectiveGenesis),
		zap.Uint64("subsidy estimated", subsidy),
		zap.Uint64("fee", fees),
		zap.Uint64("total estimated", total),
		zap.Uint64("total transferred", transferred),
		zap.Uint64("total burnt", total-transferred),
	)
	feesCount.Add(float64(fees))
	subsidyCount.Add(float64(subsidy))
	rewardsCount.Add(float64(transferred))
	burntCount.Add(float64(total - transferred))
	return result, nil
}
