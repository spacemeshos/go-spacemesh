package wallet

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

//go:generate scalegen -pkg wallet -file types_scale.go -types Arguments,SpawnArguments,SpendPayload,Nonce,SpawnPayload -imports github.com/spacemeshos/go-spacemesh/genvm/wallet

type SpawnArguments struct {
	PublicKey core.PublicKey
}

type SpendArguments struct {
	Destination core.Address
	Amount      uint64
}

type SpendPayload struct {
	Arguments SpendArguments
	Nonce     core.Nonce
	GasPrice  uint32
}

type SpawnPayload struct {
	Arguments SpawnArguments
	GasPrice  uint32
}
