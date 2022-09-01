package multisig

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
)

//go:generate scalegen

// SpawnArguments contains a collection with PublicKeys.
type SpawnArguments struct {
	PublicKeys []core.PublicKey
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
type SpendArguments = wallet.SpendArguments

// SpendPayload ...
type SpendPayload = wallet.SpendPayload

// SpawnPayload ...
type SpawnPayload struct {
	Arguments SpawnArguments
	Nonce     core.Nonce
	GasPrice  uint64
}
