package p2pcrypto

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBox(t *testing.T) {
	r := require.New(t)
	alicePrivkey, alicePubkey, err := GenerateKeyPair()
	r.NoError(err)

	bobPrivkey, bobPubkey, err := GenerateKeyPair()
	r.NoError(err)

	aliceSharedSecret := GenerateSharedSecret(alicePrivkey, bobPubkey)
	bobSharedSecret := GenerateSharedSecret(bobPrivkey, alicePubkey)
	r.Zero(bytes.Compare(aliceSharedSecret.Bytes(), bobSharedSecret.Bytes()))
	r.Equal(aliceSharedSecret.String(), bobSharedSecret.String())
	r.Equal(aliceSharedSecret.raw(), bobSharedSecret.raw())

	secretMessage := []byte("This is a secret -- sh...")
	sealed := aliceSharedSecret.Seal(secretMessage)
	opened, err := bobSharedSecret.Open(sealed)
	r.NoError(err)
	r.Equal(string(secretMessage), string(opened))
}

func TestPrependPubkey(t *testing.T) {
	r := require.New(t)
	pubkey := NewRandomPubkey()

	secretMessage := []byte("This is a secret -- sh...")
	messageWithPubkey := PrependPubkey(secretMessage, pubkey)
	message, extractedPubkey, err := ExtractPubkey(messageWithPubkey)
	r.NoError(err)
	r.Equal(string(secretMessage), string(message))
	r.Zero(bytes.Compare(pubkey.Bytes(), extractedPubkey.Bytes()))
}
