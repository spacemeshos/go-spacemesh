package vesting

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
)

func BaseGas(method uint8, signatures int) uint64 {
	if method == MethodDrainVault {
		return core.TX + core.EDVERIFY*uint64(signatures)
	}
	return multisig.BaseGas(method, signatures)
}

func ExecGas(method uint8, keys int) uint64 {
	if method == MethodDrainVault {
		gas := core.ACCOUNT_ACCESS
		gas += core.SizeGas(core.LOAD, 64)   // vault state
		gas += core.SizeGas(core.UPDATE, 16) // update nonce + amount
		gas += core.SizeGas(core.UPDATE, 16) // update amount + mutable vault state
		return gas
	}
	return multisig.ExecGas(method, keys)
}
