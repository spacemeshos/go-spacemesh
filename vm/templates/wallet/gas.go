package wallet

import (
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

func BaseGas() uint64 {
	// TODO(lane): depends on https://github.com/athenavm/athena/issues/127
	// mock for now
	return core.TX + core.ATHENA_GAS_VERIFY
}

func LoadGas() uint64 {
	return core.ACCOUNT_ACCESS + core.SizeGas(core.LOAD, core.PUBLIC_KEY_SIZE+core.ACCOUNT_HEADER_SIZE)
}
