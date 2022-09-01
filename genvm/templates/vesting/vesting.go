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

// Spend transfers an amount to the address specified in SpendArguments.
func (v *Vesting) Spend(ctx *core.Context, args *SpendArguments) error {
	return ctx.Transfer(args.Destination, args.Amount)
}
