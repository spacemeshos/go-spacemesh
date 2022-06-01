package vm

import (
	"crypto/sha256"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

func ComputePrincipal(template core.Address, nonce core.Nonce, args scale.Encodable) core.Address {
	hasher := sha256.New()
	encoder := scale.NewEncoder(hasher)
	template.EncodeScale(encoder)
	nonce.EncodeScale(encoder)
	args.EncodeScale(encoder)
	hash := hasher.Sum(nil)
	var rst core.Address
	copy(rst[:], hash)
	return rst
}
