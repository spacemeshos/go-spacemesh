package vault

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
)

// SpendArguments contains recipient and amount.
type SpendArguments = wallet.SpendArguments

//go:generate scalegen

// SpawnArguments for the vault.
type SpawnArguments struct {
	Owner               core.Address
	TotalAmount         uint64
	InitialUnlockAmount uint64
	VestingStart        core.LayerID
	VestingEnd          core.LayerID
}

func (args *SpawnArguments) String() string {
	return fmt.Sprintf("owner = %s. total = %d smidge. initial = %d smidge. start = %d. end = %d",
		args.Owner.String(), args.TotalAmount, args.InitialUnlockAmount, args.VestingStart, args.VestingEnd,
	)
}
