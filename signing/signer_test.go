package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewEdSignerFromBuffer(t *testing.T) {
	b := []byte{1, 2, 3}
	_, err := NewEdSigner(WithPrivateKey(b))
	require.ErrorContains(t, err, "too small")

	b = make([]byte, 64)
	_, err = NewEdSigner(WithPrivateKey(b))
	require.ErrorContains(t, err, "private and public do not match")
}

func TestEdSigner_Sign(t *testing.T) {
	ed, err := NewEdSigner()
	require.NoError(t, err)

	m := make([]byte, 4)
	rand.Read(m)
	sig := ed.Sign(HARE, m)
	signed := make([]byte, len(m)+1)
	signed[0] = byte(HARE)
	copy(signed[1:], m)

	ok := ed25519.Verify(ed.PublicKey().Bytes(), signed, sig[:])
	require.Truef(t, ok, "failed to verify message %x with sig %x", m, sig)
}

func TestEdSigner_ValidKeyEncoding(t *testing.T) {
	ed, err := NewEdSigner()
	require.NoError(t, err)

	require.Equal(t, []byte(ed.priv[32:]), ed.PublicKey().Bytes())
}

func TestEdSigner_WithPrivateKey(t *testing.T) {
	ed, err := NewEdSigner()
	require.NoError(t, err)

	key := ed.PrivateKey()
	ed2, err := NewEdSigner(WithPrivateKey(key))
	require.NoError(t, err)
	require.Equal(t, ed.priv, ed2.priv)
	require.Equal(t, ed.PublicKey(), ed2.PublicKey())
}

func TestPublicKey_ShortString(t *testing.T) {
	pub := NewPublicKey([]byte{1, 2, 3})
	require.Equal(t, "010203", pub.String())
	require.Equal(t, "01020", pub.ShortString())

	pub = NewPublicKey([]byte{1, 2})
	require.Equal(t, pub.String(), pub.ShortString())
}
