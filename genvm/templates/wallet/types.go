package wallet

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

//go:generate scalegen -pkg wallet -file types_scale.go -types Arguments,SpawnArguments,SpendPayload,Nonce,SpawnPayload -imports github.com/spacemeshos/go-spacemesh/genvm/wallet

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
	GasPrice  uint32
}

// SpawnPayload ...
type SpawnPayload struct {
	Arguments SpawnArguments
	GasPrice  uint32
}
