package wallet

import (
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

//go:generate scalegen

// SpawnArguments ...
type SpawnArguments struct {
	PublicKey core.PublicKey
}

// SpendArguments ...
type SpendArguments struct {
	Destination core.Address
	Amount      uint64
}
