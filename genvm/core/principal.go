package core

import (
	"crypto/sha256"

	"github.com/spacemeshos/go-scale"
)

// ComputePrincipal address as the last 20 bytes from sha256(scale(template || nonce || args)).
func ComputePrincipal(template Address, nonce Nonce, args scale.Encodable) Address {
	hasher := sha256.New()
	encoder := scale.NewEncoder(hasher)
	template.EncodeScale(encoder)
	nonce.EncodeScale(encoder)
	args.EncodeScale(encoder)
	hash := hasher.Sum(nil)
	var rst Address
	copy(rst[12:], hash)
	return rst
}
