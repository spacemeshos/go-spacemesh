package signing

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=signing -destination=./mocks.go -source=./interfaces.go

// Signer is a common interface for signature generation.
type Signer interface {
	Sign([]byte) []byte
	PublicKey() *PublicKey
	LittleEndian() bool
}

// KeyExtractor is a common interface for signature verification with support of public key extraction.
type KeyExtractor interface {
	Extract(msg, sig []byte) (*PublicKey, error)
}

type nonceFetcher interface {
	NonceForNode(types.NodeID) (types.VRFPostIndex, error)
}
