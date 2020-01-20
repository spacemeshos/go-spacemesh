package crypto

import (
	"crypto/rand"
	"errors"
	"golang.org/x/crypto/ed25519"
)

type VRFSigner struct {
	privateKey []byte
}

func (s *VRFSigner) Sign(message []byte) []byte {
	// TODO: replace with BLS (?)
	return ed25519.Sign(s.privateKey, message)
}

func NewVRFSigner(privateKey []byte) *VRFSigner {
	return &VRFSigner{privateKey: privateKey}
}

func ValidateVRF(message, signature, publicKey []byte) error {
	if !ed25519.Verify(publicKey, message, signature) {
		return errors.New("VRF validation failed")
	}
	return nil
}

func GenerateVRFKeys() (publicKey, privateKey []byte, err error) {
	return ed25519.GenerateKey(rand.Reader)
}
