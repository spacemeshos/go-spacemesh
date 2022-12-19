package signing

import (
	"testing"

	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/rand"
)

func TestNewEdSignerFromBuffer(t *testing.T) {
	b := []byte{1, 2, 3}
	_, err := NewEdSignerFromKey(b)
	assert.NotNil(t, err)
	assert.Equal(t, "buffer too small", err.Error())
	b = make([]byte, 64)
	_, err = NewEdSignerFromKey(b)
	assert.NotNil(t, err)
	assert.Equal(t, "private and public do not match", err.Error())
}

func TestEdSigner_Sign(t *testing.T) {
	ed := NewEdSigner()
	m := make([]byte, 4)
	rand.Read(m)
	sig := ed.Sign(m)
	assert.True(t, ed25519.Verify2(ed25519.PublicKey(ed.PublicKey().Bytes()), m, sig))
}

func TestNewEdSigner(t *testing.T) {
	ed := NewEdSigner()
	assert.Equal(t, []byte(ed.priv[32:]), []byte(ed.PublicKey().Bytes()))
}

func TestEdSigner_ToBytes(t *testing.T) {
	ed := NewEdSigner()
	buff := ed.PrivateKey()
	ed2, err := NewEdSignerFromKey(buff)
	assert.Nil(t, err)
	assert.Equal(t, ed.priv, ed2.priv)
	assert.Equal(t, ed.PublicKey(), ed2.PublicKey())
}

func TestPublicKey_ShortString(t *testing.T) {
	pub := NewPublicKey([]byte{1, 2, 3})
	assert.Equal(t, "010203", pub.String())
	assert.Equal(t, "01020", pub.ShortString())

	pub = NewPublicKey([]byte{1, 2})
	assert.Equal(t, pub.String(), pub.ShortString())
}

func TestPrefix(t *testing.T) {
	t.Run("signer mismatch", func(t *testing.T) {
		signer := NewEdSigner(WithPrefix([]byte("one")))
		verifier := NewEDVerifier(WithVerifierPrefix([]byte("two")))
		msg := []byte("test")
		sig := signer.Sign(msg)

		pub, err := verifier.Extract(msg, sig)
		require.NoError(t, err)
		require.NotEqual(t, pub.Bytes(), signer.PublicKey().Bytes())
	})
	t.Run("no mismatch", func(t *testing.T) {
		signer := NewEdSigner(WithPrefix([]byte("one")))
		verifier := NewEDVerifier(WithVerifierPrefix([]byte("one")))
		msg := []byte("test")
		sig := signer.Sign(msg)

		pub, err := verifier.Extract(msg, sig)
		require.NoError(t, err)
		require.Equal(t, pub.Bytes(), signer.PublicKey().Bytes())
	})
}
