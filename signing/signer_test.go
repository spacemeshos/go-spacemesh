package signing

import (
	"testing"

	"github.com/spacemeshos/ed25519"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/rand"
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
	sig := ed.Sign(m)

	ok := ed25519.Verify2(ed25519.PublicKey(ed.PublicKey().Bytes()), m, sig)
	require.Truef(t, ok, "failed to verify message %x with sig %x", m, sig)
}

func TestEdSigner_ValidKeyEncoding(t *testing.T) {
	ed, err := NewEdSigner()
	require.NoError(t, err)

	require.Equal(t, []byte(ed.priv[32:]), []byte(ed.PublicKey().Bytes()))
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
