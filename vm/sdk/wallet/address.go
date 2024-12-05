package wallet

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

// Address computes wallet address from the public key.
func Address(pub []byte) types.Address {
	if len(pub) != 32 {
		panic("invalid public key length")
	}

	// NOTE: the spawn arguments are just a [32]byte, which scale encodes "as is".
	return core.ComputePrincipalFromBlob(wallet.TemplateAddress, pub)
}
