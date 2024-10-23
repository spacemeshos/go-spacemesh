package wallet

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
)

// Address computes wallet address from the public key.
func Address(pub []byte) types.Address {
	if len(pub) != types.Hash32Length {
		panic("public key must be 32 bytes")
	}

	// Encode using the VM
	vmlib, err := athcon.LoadLibrary(host.AthenaLibPath())
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}

	athenaPayload := vmlib.EncodeTxSpawn(athcon.Bytes32(pub))
	return core.ComputePrincipal(wallet.TemplateAddress, athenaPayload)
}
