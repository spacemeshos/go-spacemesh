package signing

import (
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"
)

var _ Signer = VRFSigner{}

// VRFSigner is a signer for VRF purposes.
type VRFSigner struct {
	privateKey []byte
	pub        *PublicKey
}

// Sign signs a message for VRF purposes.
func (s VRFSigner) Sign(msg []byte) []byte {
	return ecvrf.Prove(ed25519.PrivateKey(s.privateKey), msg)
}

// PublicKey of the signer.
func (s VRFSigner) PublicKey() *PublicKey {
	return s.pub
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (s VRFSigner) LittleEndian() bool {
	return true
}

// VRFVerify verifies a message and signature, given a public key.
func VRFVerify(pub, msg, sig []byte) bool {
	valid, _ := ecvrf.Verify(ed25519.PublicKey(pub), sig, msg)
	return valid
}

var _ Verifier = VRFVerifier{}

// VRFVerifier is a verifier for VRF purposes.
type VRFVerifier struct{}

// Verify that signature matches public key.
func (VRFVerifier) Verify(pub *PublicKey, msg, sig []byte) bool {
	return VRFVerify(pub.Bytes(), msg, sig)
}
