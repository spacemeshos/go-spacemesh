package signing

import (
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// VRFSigner is a signer for VRF purposes.
type VRFSigner struct {
	privateKey []byte
	nodeID     types.NodeID
}

// Sign signs a message for VRF purposes.
func (s VRFSigner) Sign(msg []byte) []byte {
	return ecvrf.Prove(ed25519.PrivateKey(s.privateKey), msg)
}

// PublicKey of the signer.
func (s VRFSigner) PublicKey() *PublicKey {
	return NewPublicKey(s.nodeID.Bytes())
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (s VRFSigner) LittleEndian() bool {
	return true
}

// VRFVerifier is a verifier for VRF purposes.
type VRFVerifier struct{}

// Verify that signature matches public key.
func (VRFVerifier) Verify(pub *PublicKey, msg, sig []byte) bool {
	valid, _ := ecvrf.Verify(pub.Bytes(), sig, msg)
	return valid
}
