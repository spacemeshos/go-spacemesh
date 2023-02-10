package vesting

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
)

// Vesting is a mutlsig template that supports transaction to drain vault.
type Vesting struct {
	*multisig.MultiSig
}

// MaxSpend returns zero for drain vault or forwards to multisig template.
func (v *Vesting) MaxSpend(method core.Method, args any) (uint64, error) {
	if method == MethodDrainVault {
		return 0, nil
	}
	return v.MultiSig.MaxSpend(method, args)
}
