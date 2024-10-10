package types

import (
	"encoding/hex"
	"fmt"

	"go.uber.org/zap/zapcore"
)

//go:generate scalegen

// TxHeader is a transaction header, with some of the fields defined directly in the tx
// and the rest is computed by the template based on immutable state and method arguments.
type TxHeader struct {
	Principal Address

	// TODO(lane): TemplateAddress and Method are unused by the Athena VM, and should be removed.
	TemplateAddress Address
	Method          uint8

	Nonce       Nonce
	LayerLimits LayerLimits
	MaxGas      uint64
	GasPrice    uint64
	MaxSpend    uint64

	// Payload is opaque to the host (go-spacemesh), and is passed into and interpreted by the VM.
	Payload []byte
}

// Fee is a MaxGas multiplied by a GasPrice.
func (h *TxHeader) Fee() uint64 {
	return h.MaxGas * h.GasPrice
}

// Spending is Fee() + MaxSpend.
func (h *TxHeader) Spending() uint64 {
	return h.Fee() + h.MaxSpend
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MarshalLogObject implements encoding for the tx header.
func (h *TxHeader) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	payloadHash := hex.EncodeToString(h.Payload)
	encoder.AddString("principal", h.Principal.String())
	encoder.AddUint64("nonce_counter", h.Nonce)
	encoder.AddUint32("layer_min", h.LayerLimits.Min)
	encoder.AddUint32("layer_max", h.LayerLimits.Max)
	encoder.AddUint64("max_gas", h.MaxGas)
	encoder.AddUint64("gas_price", h.GasPrice)
	encoder.AddUint64("max_spend", h.MaxSpend)
	encoder.AddString("payload",
		fmt.Sprintf("%s... (len %d)", payloadHash[:min(len(payloadHash), 5)], len(h.Payload)))
	return nil
}

// LayerLimits if defined restricts in what layers transaction may be applied.
type LayerLimits struct {
	Min, Max uint32
}

// Nonce alias to uint64.
type Nonce = uint64
