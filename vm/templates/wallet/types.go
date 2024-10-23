package wallet

import (
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

//go:generate scalegen

// SpendArguments ...
type SpendArguments struct {
	Destination core.Address
	Amount      uint64
}
