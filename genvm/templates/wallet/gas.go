package wallet

import (
	"math"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

func BaseGas(method uint8) uint64 {
	switch method {
	case core.MethodSpawn:
		return core.TX + core.EDVERIFY + core.SPAWN
	case core.MethodSpend:
		return core.TX + core.EDVERIFY
	}
	return math.MaxUint64
}

func LoadGas() uint64 {
	return core.ACCOUNT_ACCESS + core.SizeGas(core.LOAD, core.PUBLIC_KEY_SIZE+core.ACCOUNT_HEADER_SIZE)
}

func ExecGas(method uint8) uint64 {
	switch method {
	case core.MethodSpawn:
		return core.SizeGas(core.STORE, core.PUBLIC_KEY_SIZE+core.ACCOUNT_HEADER_SIZE)
	case core.MethodSpend:
		gas := core.ACCOUNT_ACCESS
		gas += core.SizeGas(core.LOAD, core.ACCOUNT_BALANCE_SIZE)
		gas += core.SizeGas(core.UPDATE, core.ACCOUNT_HEADER_SIZE)
		gas += core.SizeGas(core.UPDATE, core.ACCOUNT_BALANCE_SIZE)
		return gas
	}
	return math.MaxUint64
}
