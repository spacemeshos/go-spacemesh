package mesh

import (
	"math"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Config defines the configuration options for Spacemesh rewards.
type Config struct {
	BaseReward uint64 `mapstructure:"base-reward"`
}

// DefaultMeshConfig returns the default Config.
func DefaultMeshConfig() Config {
	return Config{
		BaseReward: 50 * uint64(math.Pow10(12)),
	}
}

func calculateLayerReward(id types.LayerID, params Config) uint64 {
	// todo: add inflation rules here
	return params.BaseReward
}

func calculateActualRewards(layer types.LayerID, rewards uint64, numBlocks uint64) (perBlock uint64, remainder uint64) {
	return rewards / numBlocks, rewards % numBlocks
}
