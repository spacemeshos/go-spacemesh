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
	GenesisId types.Hash20
}

// WithGasPrice modifies GasPrice.
func WithGasPrice(price uint64) Opt {
	return func(opts *Options) {
		opts.GasPrice = price
	}
}

var (
	// TxVersion is the only version supported at genesis.
	TxVersion = scale.U8(0)

	// MethodSpawn ...
	MethodSpawn = scale.U8(core.MethodSpawn)
	// MethodSpend ...
	MethodSpend = scale.U8(core.MethodSpend)
)
