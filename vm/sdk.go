package vm

import (
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/txs"
)

// VM is the top-level VM interface, used by the node to instantiate a VM.
type VM interface {
	AccountExists(address types.Address) (bool, error)
	ApplyGenesis(genesis []types.Account) error
	SetConfig(cfg Config)
	SetLogger(logger *zap.Logger)
	mesh.VmState
	txs.VmState
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

type Opt func(VM)

func WithLogger(logger *zap.Logger) Opt {
	return func(vm VM) {
		vm.SetLogger(logger)
	}
}

func WithConfig(cfg Config) Opt {
	return func(vm VM) {
		vm.SetConfig(cfg)
	}
}
