package types

import (
	"encoding/hex"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Account represents account state at a certain layer.
type Account struct {
	Layer       LayerID
	Address     Address
	Initialized bool
	Nonce       uint64
	Balance     uint64
	Template    *scale.Address
	State       []byte
}

func (a *Account) NextNonce() uint64 {
	if a.Template == nil {
		return 0
	}
	return a.Nonce + 1
}

func (a *Account) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("layer", a.Layer.String())
	encoder.AddString("address", a.Address.String())
	encoder.AddUint64("nonce", a.Nonce)
	encoder.AddUint64("balance", a.Balance)
	if a.Template != nil {
		encoder.AddString("template", hex.EncodeToString(a.Template[:]))
	}
	return nil
}
