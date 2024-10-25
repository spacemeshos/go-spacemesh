package core

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

func SigningBody(genesis, tx []byte) []byte {
	full := make([]byte, 0, len(genesis)+len(tx))
	full = append(full, genesis...)
	full = append(full, tx...)
	return full
}

// ComputePrincipal address as the last 24 bytes of Hash(template || spawnArgs).
// See https://github.com/spacemeshos/go-spacemesh/issues/6420 for more details.
func ComputePrincipal(template types.Address, spawnArgs []byte) Address {
	hasher := hash.GetHasher()
	defer hash.PutHasher(hasher)
	hasher.Write(template[:])
	hasher.Write(spawnArgs)
	sum := hasher.Sum(nil)
	return types.GenerateAddress(sum)
}
