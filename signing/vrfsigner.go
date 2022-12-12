package signing

import (
	"encoding/binary"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"
)

// VRFSigner is a signer for VRF purposes.
type VRFSigner struct {
	privateKey []byte
	pub        *PublicKey

	nonce uint64
}

// Sign signs a message for VRF purposes.
func (s VRFSigner) Sign(msg []byte) []byte {
	nonceMsg := make([]byte, 8)
	binary.LittleEndian.PutUint64(nonceMsg, s.nonce)
	return ecvrf.Prove(ed25519.PrivateKey(s.privateKey), append(nonceMsg, msg...))
}

// PublicKey of the signer.
func (s VRFSigner) PublicKey() *PublicKey {
	return s.pub
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (s VRFSigner) LittleEndian() bool {
	return true
}

// VRFVerifier is a verifier for VRF purposes.
type VRFVerifier struct{}

// Verify that signature matches public key.
func (VRFVerifier) Verify(pub *PublicKey, nonce uint64, msg, sig []byte) bool {
	nonceMsg := make([]byte, 8)
	binary.LittleEndian.PutUint64(nonceMsg, nonce)
	valid, _ := ecvrf.Verify(pub.Bytes(), sig, append(nonceMsg, msg...))
	return valid
}
