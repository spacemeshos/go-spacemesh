package signing

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=signing -destination=./mocks.go -source=./interfaces.go

// Signer is a common interface for signature generation.
type Signer interface {
	Sign([]byte) []byte
	PublicKey() *PublicKey
	LittleEndian() bool
}

// Verifier is a common interface for signature verification.
type Verifier interface {
	Verify(pub *PublicKey, msg, sig []byte) bool
}

// VerifyExtractor is a common interface for signature verification with support of public key extraction.
type VerifyExtractor interface {
	Extract(msg, sig []byte) (*PublicKey, error)
}

// VRFNonceProvider is the interface used by the VRFSigner to retrieve ATX headers (for the nonces).
type VRFNonceProvider interface {
	VRFNonceForNode(types.NodeID) (types.VRFPostIndex, error)
}
