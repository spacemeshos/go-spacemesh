package multisig

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
)

// Address compute account address based on the size of the public keys.
func Address(template core.Address, required uint8, pubs ...[]byte) types.Address {
	args := multisig.SpawnArguments{
		Required:   required,
		PublicKeys: make([]core.PublicKey, len(pubs)),
	}
	for i := range pubs {
		copy(args.PublicKeys[i][:], pubs[i])
	}
	return core.ComputePrincipal(template, &args)
}
