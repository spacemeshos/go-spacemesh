package multisig

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

// Address computes wallet address from spawn arguments.
func Address(template types.Address, required uint8, pubkeys []core.PublicKey) types.Address {
	if len(pubkeys) < int(required) {
		panic("cannot require more than available public keys")
	}

	args := EncodeSpawnArgs(required, pubkeys)
	return core.ComputePrincipalFromBlob(template, args)
}
