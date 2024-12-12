package multisig

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
)

// Address computes wallet address from spawn arguments.
func Address(template types.Address, required uint8, pubkeys []core.PublicKey) types.Address {
	if len(pubkeys) < int(required) {
		panic("cannot require more than available public keys")
	}

	args, err := EncodeSpawnArgs(required, pubkeys)
	if err != nil {
		panic(fmt.Errorf("encoding spawn args failed: %w", err))
	}

	return core.ComputePrincipalFromBlob(template, args)
}
