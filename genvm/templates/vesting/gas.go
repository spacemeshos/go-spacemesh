package vesting

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
)

func ExecGas(method uint8, keys int) uint64 {
	if method == MethodDrainVault {
		gas := core.ACCOUNT_ACCESS
		gas += core.SizeGas(core.LOAD, 64)
		gas += core.SizeGas(core.UPDATE, 16)
		gas += core.SizeGas(core.UPDATE, 16)
		return gas
	}
	return multisig.ExecGas(method, keys)
}
