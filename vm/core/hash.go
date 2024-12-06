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

// ComputePrincipal address as the first 24 bytes of Hash(template || blob).
// See https://github.com/spacemeshos/go-spacemesh/issues/6420 for more details.
func ComputePrincipalFromBlob(template types.Address, blob []byte) types.Address {
	hasher := hash.GetHasher()
	defer hash.PutHasher(hasher)
	hasher.Write(template[:])
	hasher.Write(blob)
	sum := hasher.Sum(nil)
	var address types.Address
	copy(address[:], sum)
	return address
}

func TemplateAddress(code []byte) types.Address {
	hasher := hash.GetHasher()
	defer hash.PutHasher(hasher)
	hasher.Write(code)
	sum := hasher.Sum(nil)
	var address types.Address
	copy(address[:], sum)
	return address
}
