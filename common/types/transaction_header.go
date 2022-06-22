package types

import "github.com/spacemeshos/go-spacemesh/log"

// TxHeader is a transaction header, with some of the fields defined directly in the tx
// and the rest is computed by the template based on immutable state and method arguments.
type TxHeader struct {
	ID          TransactionID
	Principal   Address
	Template    Address
	Method      uint8
	Nonce       Nonce
	LayerLimits LayerLimits
	MaxGas      uint64
	GasPrice    uint64
	MaxSpend    uint64
}

// MarshalLogObject implements encoding for the tx header.
func (h *TxHeader) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("id", h.ID.String())
	encoder.AddString("principal", h.Principal.String())
	encoder.AddUint64("nonce_counter", h.Nonce.Counter)
	encoder.AddUint8("nonce_bitfield", h.Nonce.Bitfield)
	encoder.AddUint32("layer_min", h.LayerLimits.Min)
	encoder.AddUint32("layer_max", h.LayerLimits.Max)
	encoder.AddUint64("max_gas", h.MaxGas)
	encoder.AddUint64("gas_price", h.GasPrice)
	encoder.AddUint64("max_spend", h.MaxSpend)
	return nil
}

// LayerLimits if defined restricts in what layers transaction may be applied.
type LayerLimits struct {
	Min, Max uint32
}

//go:generate scalegen -types Nonce

// Nonce is for ordering transactions.
// TODO(dshulyak) we are using only counter until bitfield is defined.
type Nonce struct {
	Counter  uint64
	Bitfield uint8
}
