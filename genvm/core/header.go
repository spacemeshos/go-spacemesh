package core

type Header struct {
	Principal   Address
	Nonce       Nonce
	LayerLimits LayerLimits
	MaxGas      uint64
	GasPrice    uint64
	MaxSpend    uint64
}

type LayerLimits struct {
	Min, Max uint32
}

//go:generate scalegen -pkg core -file header_scale.go -types Nonce -imports github.com/spacemeshos/go-spacemesh/genvm/core

type Nonce struct {
	Counter  uint64
	Bitfield uint8
}
