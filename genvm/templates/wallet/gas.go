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
	return core.ACCOUNT_ACCESS + core.SizeGas(core.LOAD, 48)
}

func ExecGas(method uint8) uint64 {
	switch method {
	case core.MethodSpawn:
		return core.SizeGas(core.STORE, 48)
	case core.MethodSpend:
		gas := core.ACCOUNT_ACCESS
		gas += core.SizeGas(core.LOAD, 8)
		gas += core.SizeGas(core.UPDATE, 16)
		gas += core.SizeGas(core.UPDATE, 8)
		return gas
	}
	return math.MaxUint64
}
