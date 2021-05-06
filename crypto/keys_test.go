package crypto

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
)

func TestBasicApi(t *testing.T) {

	badData, _ := hex.DecodeString("1234")
	_, err := NewPublicKey(badData)
	assert.Error(t, err, "expected error for bad key data")

	_, err = NewPrivateKey(badData)
	assert.Error(t, err, "expected error for bad key data")

	_, err = NewPrivateKeyFromString("1234")
	assert.Error(t, err, "expected error for bad key data")

	priv, pub, err := GenerateKeyPair()

	assert.Nil(t, err, "failed to generate keys")
	log.Debug("priv: %s, pub: %s", priv.Pretty(), pub.Pretty())
	log.Debug("priv: %s, pub: %s", priv.String(), pub.String())

	pub1 := priv.GetPublicKey()
	assert.True(t, bytes.Equal(pub.Bytes(), pub1.Bytes()), fmt.Sprintf("expected same pub key, %s, %s",
		pub.String(), pub1.String()))

	// serialization tests
	priv1, err := NewPrivateKey(priv.Bytes())
	assert.NoError(t, err, "unexpected error")
	assert.True(t, bytes.Equal(priv1.Bytes(), priv.Bytes()), fmt.Sprintf("expected same private key, %s, %s",
		priv1.String(), priv.String()))

	priv2, err := NewPrivateKeyFromString(priv.String())
	assert.NoError(t, err, "unexpected error")
	assert.True(t, bytes.Equal(priv2.Bytes(), priv.Bytes()), fmt.Sprintf("expected same private key, %s, %s",
		priv2.String(), priv.String()))

	pub2, err := NewPublicKey(pub.Bytes())
	assert.Nil(t, err, fmt.Sprintf("New pub key from bin error: %v", err))

	assert.True(t, bytes.Equal(pub2.Bytes(), pub.Bytes()), fmt.Sprintf("expected same public key, %s, %s",
		pub2.String(), pub.String()))

	pub3, err := NewPublicKeyFromString(pub.String())

	assert.Nil(t, err, fmt.Sprintf("New pub key from bin error: %v", err))

	assert.True(t, bytes.Equal(pub3.Bytes(), pub.Bytes()), fmt.Sprintf("Expected same public key, %s, %s",
		pub3.String(), pub.String()))
}

func TestCryptoApi(t *testing.T) {

	priv, pub, err := GenerateKeyPair()

	assert.Nil(t, err, "Failed to generate keys")

	const msg = "hello world"
	msgData := []byte(msg)

	// test signatures
	signature := priv.Sign(msgData)

	assert.Nil(t, err, fmt.Sprintf("signing error: %v", err))
	ok, err := pub.Verify(msgData, signature)
	assert.Nil(t, err, fmt.Sprintf("sign verification error: %v", err))

	assert.True(t, ok, "Failed to verify signature")

	ok, err = pub.VerifyString(msgData, hex.EncodeToString(signature))
	assert.Nil(t, err, fmt.Sprintf("sign verification error: %v", err))
	assert.True(t, ok, "Failed to verify signature")

	_, pub2, _ := GenerateKeyPair()
	ok, err = pub2.Verify(msgData, signature)
	assert.NoError(t, err)
	assert.False(t, ok)

	ok, err = pub2.VerifyString(msgData, hex.EncodeToString(signature))
	assert.Nil(t, err, fmt.Sprintf("sign verification error: %v", err))
	assert.False(t, ok, "succeed to verify wrong signature")

	// test encrypting a message for pub by pub - anyone w pub can do that
	cypherText, err := pub.Encrypt(msgData)

	assert.Nil(t, err, fmt.Sprintf("enc error: %v", err))

	// test decryption
	clearText, err := priv.Decrypt(cypherText)
	assert.Nil(t, err, fmt.Sprintf("dec error: %v", err))

	assert.True(t, bytes.Equal(msgData, clearText), "expected same dec message")

}

func BenchmarkVerify(b *testing.B) {
	b.StopTimer()

	priv, pub, err := GenerateKeyPair()

	assert.Nil(b, err, "Failed to generate keys")

	const msg = "hello world"
	msgData := []byte(msg)

	// test signatures
	signature := priv.Sign(msgData)

	assert.Nil(b, err, fmt.Sprintf("signing error: %v", err))

	b.StartTimer()
	for i := 0; i < b.N; i++ {
		//ok, err := pub.Verify(msgData, signature)
		pub.Verify(msgData, signature)

	}
	b.StopTimer()

}
