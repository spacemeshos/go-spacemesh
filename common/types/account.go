package types

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen

// Account represents account state at a certain layer.
type Account struct {
	Layer       LayerID
	Address     Address
	Initialized bool
	Nonce       uint64
	Balance     uint64
	Template    *Address
	State       []byte
}

// NextNonce returns next expected nonce for the account state.
func (a *Account) NextNonce() uint64 {
	if a.Template == nil {
		return 0
	}
	return a.Nonce + 1
}

// MarshalLogObject implements encoding for the account state.
func (a *Account) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("layer", a.Layer.String())
	encoder.AddString("principal", a.Address.String())
	encoder.AddUint64("nonce", a.Nonce)
	encoder.AddUint64("next nonce", a.NextNonce())
	encoder.AddUint64("balance", a.Balance)
	if a.Template != nil {
		encoder.AddString("template", a.Template.String())
	}
	return nil
}
