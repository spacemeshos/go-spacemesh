package mesh

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
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

func calculateActualRewards(layer types.LayerID, rewards *big.Int, numBlocks *big.Int) (*big.Int, *big.Int) {
	div, mod := new(big.Int).DivMod(rewards, numBlocks, new(big.Int))
	return div, mod
}
