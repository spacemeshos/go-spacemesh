package wallet

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
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

// SpendPayload ...
type SpendPayload struct {
	Arguments SpendArguments
	Nonce     core.Nonce
	GasPrice  uint64
}

// SpawnPayload ...
type SpawnPayload struct {
	Arguments SpawnArguments
	Nonce     core.Nonce
	GasPrice  uint64
}
