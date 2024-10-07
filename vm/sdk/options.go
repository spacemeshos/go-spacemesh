package sdk

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
)
