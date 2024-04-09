package multisig

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
)

//go:generate scalegen

// SpawnArguments contains a collection with PublicKeys.
type SpawnArguments struct {
	Required   uint8
	PublicKeys []core.PublicKey `scale:"max=10"` // update StorageLimit if it changes.
}

func (s *SpawnArguments) String() string {
	builder := bytes.NewBuffer(nil)
	builder.WriteString(fmt.Sprintf("required=%d\n", s.Required))
	for i, pub := range s.PublicKeys {
		builder.WriteString(fmt.Sprintf("%d : %s\n", i, pub.String()))
	}
	return builder.String()
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
