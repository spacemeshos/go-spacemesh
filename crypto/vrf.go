package crypto

import (
	"bytes"
	"errors"
)

type VRFSigner struct {
	privateKey []byte
}

func (s *VRFSigner) Sign(message []byte) []byte {
	// TODO: implement
	return message
}

func NewVRFSigner(privateKey []byte) *VRFSigner {
	return &VRFSigner{privateKey: privateKey}
}

func ValidateVRF(message, signature, publicKey []byte) error {
	if !bytes.Equal(message, signature) {
		return errors.New("VRF validation failed")
	}
	return nil
}
