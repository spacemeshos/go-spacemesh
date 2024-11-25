package wallet

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

// Address computes wallet address from the public key.
func Address(pub signing.PublicKey) types.Address {
	return core.ComputePrincipalFromPubkey(wallet.TemplateAddress, pub)
}
