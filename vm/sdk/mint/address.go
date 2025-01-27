package mint

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates/mint"
)

// Address computes wallet address from spawn arguments.
func Address(supply, price uint64, pubkey core.PublicKey) types.Address {
	args := EncodeSpawnArgs(supply, price, pubkey)
	return core.ComputePrincipalFromBlob(mint.TemplateAddress, args)
}
