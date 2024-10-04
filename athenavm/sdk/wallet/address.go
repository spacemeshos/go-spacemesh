package wallet

import (
	"github.com/spacemeshos/go-spacemesh/athenavm/core"
	"github.com/spacemeshos/go-spacemesh/athenavm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Address computes wallet address from the public key.
func Address(pub []byte) types.Address {
	if len(pub) != 32 {
		panic("public key must be 32 bytes")
	}
	args := wallet.SpawnArguments{}
	copy(args.PublicKey[:], pub)
	return core.ComputePrincipal(wallet.TemplateAddress, &args)
}
