package vm

import (
	"fmt"
	"math/big"

	"github.com/spacemeshos/economics/rewards"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/log"
)

func (v *VM) addRewards(lctx ApplyContext, ss *core.StagedCache, fees uint64, blockRewards []types.CoinbaseReward) ([]types.Reward, error) {
	var (
		layersAfterEffectiveGenesis = lctx.Layer.Difference(types.FirstEffectiveGenesis())
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

		v.logger.With().Debug("rewards for coinbase",
			lctx.Layer,
			blockReward.Coinbase,
			log.Stringer("relative weight", &blockReward.Weight),
			log.Uint64("subsidy", subsidyReward.Uint64()),
			log.Uint64("total", totalReward.Uint64()),
		)

		reward := types.Reward{
			Layer:       lctx.Layer,
			Coinbase:    blockReward.Coinbase,
			TotalReward: totalReward.Uint64(),
			LayerReward: subsidyReward.Uint64(),
		}
		result = append(result, reward)
		account, err := ss.Get(blockReward.Coinbase)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
		account.Balance += reward.TotalReward
		if err := ss.Update(account); err != nil {
			return nil, fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
		transferred += totalReward.Uint64()
	}
	v.logger.With().Debug("rewards for layer",
		lctx.Layer,
		log.Uint32("after genesis", layersAfterEffectiveGenesis),
		log.Uint64("subsidy estimated", subsidy),
		log.Uint64("fee", fees),
		log.Uint64("total estimated", total),
		log.Uint64("total transffered", transferred),
		log.Uint64("total burnt", total-transferred),
	)
	feesCount.Add(float64(fees))
	subsidyCount.Add(float64(subsidy))
	rewardsCount.Add(float64(transferred))
	burntCount.Add(float64(total - transferred))
	return result, nil
}
