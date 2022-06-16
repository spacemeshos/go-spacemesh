package sdk

import (
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
	Principal *core.Address
	GasPrice  uint64
	Nonce     *core.Nonce
}

// WithPrincipal provides principal address.
func WithPrincipal(address types.Address) Opt {
	return func(opts *Options) {
		opts.Principal = &address
	}
}

// WithGasPrice modifies GasPrice.
func WithGasPrice(price uint64) Opt {
	return func(opts *Options) {
		opts.GasPrice = price
	}
}

// WithNonce sets nonce for the transaction.
func WithNonce(nonce types.Nonce) Opt {
	return func(opts *Options) {
		opts.Nonce = &nonce
	}
}
