package multisig

import "github.com/spacemeshos/go-spacemesh/genvm/core"

//go:generate scalegen

// SpawnArguments ...
type SpawnArguments struct {
	PublicKeys []core.PublicKey `scale:"type=StructArray"`
}

type Signatures []Part

type Part struct {
	Ref uint64
	Sig core.Signature
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
	GasPrice  uint64
}
