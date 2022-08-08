package multisig

import "github.com/spacemeshos/go-spacemesh/genvm/core"

//go:generate scalegen

// SpawnArguments contains a collection with PublicKeys.
type SpawnArguments struct {
	PublicKeys []core.PublicKey `scale:"type=StructArray"`
}

// Signatures is a collections of parts that must satisfy multisig
// threshold requirement.
type Signatures []Part

// Part contains a reference to public key and signature from private key counterpart.
type Part struct {
	Ref uint8
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
