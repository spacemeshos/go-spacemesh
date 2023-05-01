package core

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

func SigningBody(genesis, tx []byte) []byte {
	full := make([]byte, 0, len(genesis)+len(tx))
	full = append(full, genesis...)
	full = append(full, tx...)
	return full
}

// ComputePrincipal address as the last 20 bytes from blake3(scale(template || args)).
func ComputePrincipal(template Address, args scale.Encodable) Address {
	hasher := hash.New()
	encoder := scale.NewEncoder(hasher)
	template.EncodeScale(encoder)
	args.EncodeScale(encoder)
	hash := hasher.Sum(nil)
	rst := types.GenerateAddress(hash[12:])
	return rst
}
