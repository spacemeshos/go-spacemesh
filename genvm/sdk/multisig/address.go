package multisig

import (
	"strconv"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
)

// Address compute account address based on the size of the public keys.
func Address(pubs ...[]byte) types.Address {
	var template types.Address
	switch len(pubs) {
	case 3:
		template = multisig.TemplateAddress23
	case 5:
		template = multisig.TemplateAddress35
	default:
		panic("no type for a collection of size " + strconv.Itoa(len(pubs)))
	}
	args := multisig.SpawnArguments{PublicKeys: make([]core.PublicKey, len(pubs))}
	for i := range pubs {
		copy(args.PublicKeys[i][:], pubs[i])
	}
	return core.ComputePrincipal(template, &args)
}
