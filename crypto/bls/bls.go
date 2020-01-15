package bls

import (
	"errors"
	"fmt"
	herumiBLS "github.com/herumi/bls-eth-go-binary/bls"
	"log"
)

var (
	SecKeyLen = 32
	PubKeyLen = 48
	SigLen    = 96
)

func init() {
	if err := herumiBLS.Init(herumiBLS.BLS12_381); err != nil {
		log.Fatalf("Failed to initialize BLS12-381 curve: %v", err)
	}
}

// GenKeyPair generates a new random BLS12-381 key pair.
func GenKeyPair() ([]byte, []byte) {
	sec := GenSecretKey()
	pub := sec.GetPublicKey()

	return sec.Serialize(), pub.Serialize()
}

// GenSecretKey generates a new random BLS12-381 secret key.
func GenSecretKey() herumiBLS.SecretKey {
	var sec herumiBLS.SecretKey
	sec.SetByCSPRNG()

	return sec
}

type BlsSigner struct {
	sec herumiBLS.SecretKey
}

// NewSigner returns a new BLS12-381 signer for a given secret key.
func NewSigner(sec []byte) (*BlsSigner, error) {
	var nativeSec herumiBLS.SecretKey
	if err := nativeSec.Deserialize(sec); err != nil {
		return nil, fmt.Errorf("failed to deserialize secret key: %v", err)
	}

	return &BlsSigner{sec: nativeSec}, nil
}

// Sign signs a message with BLS12-381.
func (bs *BlsSigner) Sign(msg []byte) ([]byte, error) {
	if msg == nil {
		return nil, errors.New("sign failed: nil message")
	}

	sig := bs.sec.Sign(string(msg))

	return sig.Serialize(), nil
}

// Verify verifies a BLS12-381 signature given the message, the signature and the public key.
func Verify(msg, sig, pub []byte) (bool, error) {
	if len(pub) != PubKeyLen {
		return false, fmt.Errorf("invalid public key length, expected: %v, actual: %v", PubKeyLen, len(pub))
	}

	if len(sig) != SigLen {
		return false, fmt.Errorf("invalid signature length, expected: %v, actual: %v", SigLen, len(sig))
	}

	var nativeSig herumiBLS.Sign
	if err := nativeSig.Deserialize(sig); err != nil {
		return false, fmt.Errorf("failed to deserialize signature: %v", err)
	}

	var nativePub herumiBLS.PublicKey
	if err := nativePub.Deserialize(pub); err != nil {
		return false, fmt.Errorf("failed to deserialize public key: %v", err)
	}

	ok := nativeSig.Verify(&nativePub, string(msg))

	return ok, nil
}
