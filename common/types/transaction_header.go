package types

// TxHeader is a transaction header, with some of the fields defined directly in the tx
// and the rest is computed by the template based on immutable state and method arguments.
type TxHeader struct {
	Principal   Address
	Nonce       Nonce
	LayerLimits LayerLimits
	MaxGas      uint64
	GasPrice    uint64
	MaxSpend    uint64
}

// LayerLimits if defined restricts in what layers transaction may be applied.
type LayerLimits struct {
	Min, Max uint32
}

//go:generate scalegen -pkg types -file transaction_header_scale.go -types Nonce -imports github.com/spacemeshos/go-spacemesh/common/types

// Nonce is for ordering transactions.
// TODO(dshulyak) we are using only counter until bitfield is defined.
type Nonce struct {
	Counter  uint64
	Bitfield uint8
}
