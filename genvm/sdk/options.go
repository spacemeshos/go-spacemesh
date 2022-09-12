package sdk

// Opt modifies Options.
type Opt func(*Options)

// Defaults returns default Options.
func Defaults() *Options {
	return &Options{GasPrice: 1}
}

// Options to modify common transaction fields.
type Options struct {
	GasPrice  uint64
	GenesisId [20]byte
}

// WithGasPrice modifies GasPrice.
func WithGasPrice(price uint64) Opt {
	return func(opts *Options) {
		opts.GasPrice = price
	}
}

func WithGenesisId(id [20]byte) Opt {
	return func(opts *Options) {
		opts.GenesisId = id
	}
}
