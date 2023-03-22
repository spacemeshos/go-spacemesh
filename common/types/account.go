package types

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen

// Account represents account state at a certain layer.
type Account struct {
	Layer           LayerID
	Address         Address
	Initialized     bool
	NextNonce       uint64
	Balance         uint64
	TemplateAddress *Address
	State           []byte `scale:"max=10000"`
}

// MarshalLogObject implements encoding for the account state.
func (a *Account) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("layer", a.Layer.String())
	encoder.AddString("principal", a.Address.String())
	encoder.AddUint64("next nonce", a.NextNonce)
	encoder.AddUint64("balance", a.Balance)
	if a.TemplateAddress != nil {
		encoder.AddString("template", a.TemplateAddress.String())
	}
	return nil
}
