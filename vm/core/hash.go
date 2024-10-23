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

// ComputePrincipal address as the last 20 bytes from blake3(template || spawn_args).
func ComputePrincipal(template Address, spawnArgs []byte) Address {
	hasher := hash.GetHasher()
	defer hash.PutHasher(hasher)
	hasher.Write(template[:])
	hasher.Write(spawnArgs)
	sum := hasher.Sum(nil)
	return types.GenerateAddress(sum[12:])
}
