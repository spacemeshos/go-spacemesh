package multisig

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

// Address computes wallet address from spawn arguments.
func Address(required uint8, pubkeys []core.PublicKey) types.Address {
	if len(pubkeys) < int(required) {
		panic("cannot require more than available public keys")
	}

	args, err := encodeSpawnArgs(required, pubkeys)
	if err != nil {
		panic(fmt.Errorf("encoding spawn args failed: %w", err))
	}

	return core.ComputePrincipalFromBlob(wallet.TemplateAddress, args)
}
