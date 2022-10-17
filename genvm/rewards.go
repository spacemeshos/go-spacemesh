package vm

import (
	"fmt"
	"math/big"

	erewards "github.com/spacemeshos/economics/rewards"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
)

// ValidateRewards syntactically validates rewards.
func ValidateRewards(rewards []types.AnyReward) error {
	if len(rewards) == 0 {
		return fmt.Errorf("empty rewards")
	}
	unique := map[core.Address]struct{}{}
	for _, reward := range rewards {
		if reward.Weight.Num == 0 || reward.Weight.Denom == 0 {
			return fmt.Errorf("reward with invalid (zeroed) weight (%d/%d) included into the block for %v", reward.Weight.Num, reward.Weight.Denom, reward.Coinbase)
		}
		if _, exists := unique[reward.Coinbase]; exists {
			return fmt.Errorf("multiple rewards for the same coinbase %v", reward.Coinbase)
		}
		unique[reward.Coinbase] = struct{}{}
	}
	return nil
}

func (v *VM) addRewards(lctx ApplyContext, ss *core.StagedCache, tx *sql.Tx, fees uint64, blockRewards []types.AnyReward) error {
	var (
		layersAfterEffectiveGenesis = lctx.Layer.Difference(types.GetEffectiveGenesis())
		subsidy                     = erewards.TotalSubsidyAtLayer(layersAfterEffectiveGenesis)
		total                       = subsidy + fees
		transferred                 uint64
		totalWeight                 = util.WeightFromUint64(0)
	)
	for _, blockReward := range blockRewards {
		totalWeight.Add(util.WeightFromNumDenom(blockReward.Weight.Num, blockReward.Weight.Denom))
	}
	for _, blockReward := range blockRewards {
		relative := util.WeightFromNumDenom(blockReward.Weight.Num, blockReward.Weight.Denom).Div(totalWeight)

		totalReward := new(big.Int).SetUint64(total)
		totalReward.
			Mul(totalReward, relative.Num()).
			Quo(totalReward, relative.Denom())
		if !totalReward.IsUint64() {
			return fmt.Errorf("%w: total reward %v for %v overflows uint64",
				core.ErrInternal, totalReward, blockReward.Coinbase)
		}

		subsidyReward := new(big.Int).SetUint64(subsidy)
		subsidyReward.
			Mul(subsidyReward, relative.Num()).
			Quo(subsidyReward, relative.Denom())
		if !subsidyReward.IsUint64() {
			return fmt.Errorf("%w: subsidy reward %v for %v overflows uint64",
				core.ErrInternal, subsidyReward, blockReward.Coinbase)
		}

		v.logger.With().Debug("rewards for coinbase",
			lctx.Layer,
			blockReward.Coinbase,
			log.Stringer("relative weight", &blockReward.Weight),
			log.Uint64("subsidy", subsidyReward.Uint64()),
			log.Uint64("total", totalReward.Uint64()),
		)

		reward := &types.Reward{
			Layer:       lctx.Layer,
			Coinbase:    blockReward.Coinbase,
			TotalReward: totalReward.Uint64(),
			LayerReward: subsidyReward.Uint64(),
		}
		if err := rewards.Add(tx, reward); err != nil {
			return fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
		account, err := ss.Get(blockReward.Coinbase)
		if err != nil {
			return fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
		}
		account.Balance += reward.TotalReward
		if err := ss.Update(account); err != nil {
			return fmt.Errorf("%w: %s", core.ErrInternal, err.Error())
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
	return nil
}
