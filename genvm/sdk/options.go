package sdk

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

// Opt modifies Options.
type Opt func(*Options)

// Defaults returns default Options.
func Defaults() *Options {
	return &Options{GasPrice: 1}
}

// Options to modify common transaction fields.
type Options struct {
	GasPrice  uint64
	GenesisID types.Hash20
}

// WithGasPrice modifies GasPrice.
func WithGasPrice(price uint64) Opt {
	return func(opts *Options) {
		opts.GasPrice = price
	}
}

// WithGenesisID updates genesis id that will be used to prefix tx hash.
func WithGenesisID(id types.Hash20) Opt {
	return func(opts *Options) {
		opts.GenesisID = id
	}
}

var (
	// TxVersion is the only version supported at genesis.
	TxVersion = scale.U8(0)
	// MethodSpend ...
	MethodSpend = scale.U8(core.MethodSpend)
)

var (
	SelfSpawn         = scale.U8(core.SelfSpawn)
	Spawn             = scale.U8(core.Spawn)
	LocalMethodCall   = scale.U8(core.LocalMethodCall)
	ForeignMethodCall = scale.U8(core.ForeignMethodCall)
)
