package vesting

import "github.com/spacemeshos/go-spacemesh/genvm/core"

type Vesting struct {
	core.Template
}

func (v *Vesting) MaxSpend(method uint8, args any) (uint64, error) {
	if method == MethodDrainVault {
		return 0, nil
	}
	return v.Template.MaxSpend(method, args)
}
