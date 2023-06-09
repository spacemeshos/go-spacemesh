package vesting

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
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
		gas += core.SizeGas(core.LOAD, vault.VAULT_STATE_SIZE)
		gas += core.SizeGas(core.UPDATE, core.ACCOUNT_HEADER_SIZE)
		gas += core.SizeGas(core.UPDATE, core.ACCOUNT_BALANCE_SIZE+vault.DRAINED_SIZE)
		return gas
	}
	return multisig.ExecGas(method, keys)
}
