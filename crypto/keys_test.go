package crypto

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/log"
	"testing"
)

func TestBasicApi(t *testing.T) {

	priv, pub, err := GenerateKeyPair()

	assert.Nil(t, err, "failed to generate keys")
	log.Info("priv: %s, pub: %s", priv.Pretty(), pub.Pretty())
	log.Info("priv: %s, pub: %s", priv.String(), pub.String())

	pub1 := priv.GetPublicKey()
	assert.True(t, bytes.Equal(pub.Bytes(), pub1.Bytes()), fmt.Sprintf("expected same pub key, %s, %s",
		pub.String(), pub1.String()))

	// serialization tests
	priv1 := NewPrivateKey(priv.Bytes())
	assert.True(t, bytes.Equal(priv1.Bytes(), priv.Bytes()), fmt.Sprintf("expected same private key, %s, %s",
		priv1.String(), priv.String()))

	priv2 := NewPrivateKeyFromString(priv.String())
	assert.True(t, bytes.Equal(priv2.Bytes(), priv.Bytes()), fmt.Sprintf("expected same private key, %s, %s",
		priv2.String(), priv.String()))

	pub2, err := NewPublicKey(pub.Bytes())
	assert.Nil(t, err, fmt.Sprintf("New pub key from bin error: %v", err))

	assert.True(t, bytes.Equal(pub2.Bytes(), pub.Bytes()), fmt.Sprintf("expected same public key, %s, %s",
		pub2.String(), pub.String()))

	pub3, err := NewPublicKeyFromString(pub.String())
	assert.Nil(t, err, fmt.Sprintf("New pub key from bin error: %v", err))

	assert.True(t, bytes.Equal(pub3.Bytes(), pub.Bytes()), fmt.Sprintf("expected same public key, %s, %s",
		pub3.String(), pub.String()))
}

func TestCryptoApi(t *testing.T) {

	priv, pub, err := GenerateKeyPair()

	assert.Nil(t, err, "failed to generate keys")

	const msg = "hello world"
	msgData := []byte(msg)

	// test signatures
	signature, err := priv.Sign(msgData)
	assert.Nil(t, err, fmt.Sprintf("Signing error: %v", err))
	ok, err := pub.Verify(msgData, signature)
	assert.Nil(t, err, fmt.Sprintf("Sign verification error: %v", err))
	assert.True(t, ok, "Failed to verify signature")

	// test encrypting a message for pub by pub - anyone w pub can do that
	cypherText, err := pub.Encrypt(msgData)
	assert.Nil(t, err, fmt.Sprintf("Enc error: %v", err))

	// test decryption
	clearText, err := priv.Decrypt(cypherText)
	assert.Nil(t, err, fmt.Sprintf("Dec error: %v", err))
	assert.True(t, bytes.Equal(msgData, clearText), "expected same dec message")

}
