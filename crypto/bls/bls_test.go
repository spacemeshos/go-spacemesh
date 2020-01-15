package bls

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBLS(t *testing.T) {
	req := require.New(t)

	// Generate key pair.
	sec, pub := GenKeyPair()
	req.Len(sec, SecKeyLen)
	req.Len(pub, PubKeyLen)

	// Sign.
	signer, err := NewSigner(sec)
	req.NotNil(signer)
	req.NoError(err)
	msg := []byte("this is a message")
	sig, err := signer.Sign(msg)
	req.NoError(err)
	req.Len(sig, SigLen)

	// Verify signature.
	ok, err := Verify(msg, sig, pub)
	req.NoError(err)
	req.True(ok)
}

func TestBLS_Randomness(t *testing.T) {
	req := require.New(t)

	n := 100
	set := make(map[string]bool)

	// Verify n distinct secret keys.
	for i := 0; i < n; i++ {
		sec, _ := GenKeyPair()
		secHex := fmt.Sprintf("%x", sec)
		_, ok := set[secHex]
		req.False(ok, "random secret key was repeated")

		set[secHex] = true
	}
}
