package tokenwallet

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	tokenwallet "github.com/spacemeshos/go-spacemesh/vm/templates/token_wallet"
)

// Address computes wallet address from spawn arguments.
func Address(mintTemplate types.Address, pubkey core.PublicKey) types.Address {
	args := EncodeSpawnArgs(mintTemplate, tokenwallet.TemplateAddress, pubkey)
	return core.ComputePrincipalFromBlob(tokenwallet.TemplateAddress, args)
}
