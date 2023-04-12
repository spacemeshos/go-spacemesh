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
func (v *Vesting) MaxSpend(method uint8, args any) (uint64, error) {
	if method == MethodDrainVault {
		return 0, nil
	}
	return v.MultiSig.MaxSpend(method, args)
}

func (v *Vesting) ExecGas(method uint8) uint64 {
	if method == MethodDrainVault {
		gas := core.ACCOUNT_ACCESS
		gas += core.SizeGas(core.LOAD, 64)
		gas += core.SizeGas(core.UPDATE, 16)
		gas += core.SizeGas(core.UPDATE, 16)
		return gas
	}
	return v.MultiSig.ExecGas(method)
}
