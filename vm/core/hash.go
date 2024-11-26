package core

import (
	"fmt"

	"github.com/ChainSafe/gossamer/pkg/scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

func SigningBody(genesis, tx []byte) []byte {
	full := make([]byte, 0, len(genesis)+len(tx))
	full = append(full, genesis...)
	full = append(full, tx...)
	return full
}

// ComputePrincipal address as the last 24 bytes of Hash(template || blob).
// See https://github.com/spacemeshos/go-spacemesh/issues/6420 for more details.
func ComputePrincipalFromBlob(template types.Address, blob []byte) Address {
	hasher := hash.GetHasher()
	defer hash.PutHasher(hasher)
	hasher.Write(template[:])
	hasher.Write(blob)
	sum := hasher.Sum(nil)
	return types.GenerateAddress(sum)
}

func ComputePrincipalFromPubkey(template types.Address, pubkey []byte) Address {
	// construct and encode the blob, which is a SCALE-encoded Athena wallet template instance
	blob, err := scale.Marshal(struct {
		Nonce, Balance uint64
		Owner          [32]byte
	}{0, 0, [32]byte(pubkey)})
	if err != nil {
		panic(fmt.Sprintf("scale-encoding spawn args failed: %s", err))
	}
	return ComputePrincipalFromBlob(template, blob)
}
