package mesh

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"math"
	"math/big"
)

// Config defines the configuration options for Spacemesh rewards.
type Config struct {
	BaseReward *big.Int `mapstructure:"base-reward"`
}

// DefaultMeshConfig returns the default Config.
func DefaultMeshConfig() Config {
	return Config{
		BaseReward: big.NewInt(50 * int64(math.Pow10(12))),
	}
}

func calculateLayerReward(id types.LayerID, params Config) *big.Int {
	// todo: add inflation rules here
	return params.BaseReward
}

func calculateActualRewards(layer types.LayerID, rewards *big.Int, numBlocks *big.Int) *big.Int {
	div, mod := new(big.Int).DivMod(rewards, numBlocks, new(big.Int))
	log.With().Info("Reward calculated",
		log.LayerID(uint64(layer)),
		log.Uint64("total_reward", rewards.Uint64()),
		log.Uint64("num_blocks", numBlocks.Uint64()),
		log.Uint64("block_reward", div.Uint64()),
		log.Uint64("reward_remainder", mod.Uint64()),
	)
	return div
}
