package vm

import (
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// VM is the top-level VM interface, used by the node to instantiate a VM.
type VM interface {
	DefaultConfig() Config
	SetConfig(cfg Config)
	SetLogger(logger *zap.Logger)
}

// Config defines the configuration options for vm.
type Config struct {
	GasLimit  uint64
	GenesisID types.Hash20
	Module    string
}

// DefaultConfig returns the default RewardConfig.
func DefaultConfig() Config {
	return Config{
		GasLimit: 100_000_000,
		Module:   "genvm",
	}
}
