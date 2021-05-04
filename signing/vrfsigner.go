package signing

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/ed25519"
)

// VRFSigner is a signer for VRF purposes
type VRFSigner struct {
	privKey []byte
}

// Sign signs a message for VRF purposes
func (s VRFSigner) Sign(m []byte) ([]byte, error) {
	return ed25519.Sign(s.privKey, m), nil
}

// NewVRFSigner creates a new VRFSigner from a 32-byte seed
func NewVRFSigner(seed []byte) (*VRFSigner, []byte, error) {
	if len(seed) < ed25519.SeedSize {
		return nil, nil, fmt.Errorf("seed must be >=%d bytes (len(seed)=%d)", ed25519.SeedSize, len(seed))
	}
	vrfPub, vrfPriv, err := ed25519.GenerateKey(bytes.NewReader(seed))
	if err != nil {
		return nil, nil, err
	}
	return &VRFSigner{privKey: vrfPriv}, vrfPub, nil
}

// VRFVerify verifies a message and signature, given a public key
func VRFVerify(msg, sig, pub []byte) (bool, error) {
	return ed25519.Verify(pub, msg, sig), nil
}
