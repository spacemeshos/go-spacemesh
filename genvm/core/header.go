package core

type Header struct {
	Nonce Nonce
	Layer struct {
		Min, Max uint32
	}
	MaxGas   uint64
	GasPrice uint64
	MaxSpend uint64
}

//go:generate scalegen -pkg core -file header_scale.go -types Nonce -imports github.com/spacemeshos/go-spacemesh/core

type Nonce struct {
	Counter  uint64
	Bitfield uint8
}
