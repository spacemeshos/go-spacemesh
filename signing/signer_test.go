package signing

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/stretchr/testify/require"
)

func Test_NewEdSigner_WithPrivateKey(t *testing.T) {
	t.Run("key too short", func(t *testing.T) {
		_, err := NewEdSigner(WithPrivateKey(make([]byte, 63)))
		require.ErrorContains(t, err, "invalid key length")
	})

	t.Run("key too long", func(t *testing.T) {
		_, err := NewEdSigner(WithPrivateKey(make([]byte, 65)))
		require.ErrorContains(t, err, "invalid key length")
	})

	t.Run("key mismatch", func(t *testing.T) {
		_, err := NewEdSigner(WithPrivateKey(make([]byte, 64)))
		require.ErrorContains(t, err, "private and public do not match")
	})

	t.Run("valid key", func(t *testing.T) {
		ed, err := NewEdSigner()
		require.Empty(t, ed.Name())
		require.NoError(t, err)

		key := ed.PrivateKey()
		ed2, err := NewEdSigner(WithPrivateKey(key))
		require.NoError(t, err)
		require.Equal(t, ed.priv, ed2.priv)
		require.Equal(t, ed.PublicKey(), ed2.PublicKey())
		require.Empty(t, ed2.Name())
	})

	t.Run("fails if private key already set", func(t *testing.T) {
		ed, err := NewEdSigner()
		require.NoError(t, err)

		key := ed.PrivateKey()
		_, err = NewEdSigner(WithPrivateKey(key), WithPrivateKey(key))
		require.ErrorContains(t, err, "invalid option WithPrivateKey: private key already set")

		keyFile := filepath.Join(t.TempDir(), "identity.key")
		dst := make([]byte, hex.EncodedLen(len(ed.PrivateKey())))
		hex.Encode(dst, ed.PrivateKey())
		err = os.WriteFile(keyFile, dst, 0o600)
		require.NoError(t, err)

		_, err = NewEdSigner(FromFile(keyFile), WithPrivateKey(key))
		require.ErrorContains(t, err, "invalid option WithPrivateKey: private key already set")
	})
}

func Test_NewEdSigner_FromFile(t *testing.T) {
	t.Run("invalid file", func(t *testing.T) {
		_, err := NewEdSigner(FromFile("nonexistent"))
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.ErrorContains(t, err, "failed to open identity file at nonexistent")
	})

	t.Run("invalid key", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "identity.key")
		key := bytes.Repeat([]byte{0}, PrivateKeySize*2)
		err := os.WriteFile(keyFile, key, 0o600)
		require.NoError(t, err)

		_, err = NewEdSigner(FromFile(keyFile))
		require.ErrorContains(t, err, "decoding private key in identity.key")
	})

	t.Run("invalid key size - too short", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "identity.key")
		key := bytes.Repeat([]byte{0}, 63)
		dst := make([]byte, hex.EncodedLen(len(key)))
		hex.Encode(dst, key)
		err := os.WriteFile(keyFile, dst, 0o600)
		require.NoError(t, err)

		_, err = NewEdSigner(FromFile(keyFile))
		require.ErrorContains(t, err, "invalid key size 63/64 for identity.key")
	})

	t.Run("invalid key size - too long", func(t *testing.T) {
		keyFile := filepath.Join(t.TempDir(), "identity.key")
		key := bytes.Repeat([]byte{0}, 65)
		dst := make([]byte, hex.EncodedLen(len(key)))
		hex.Encode(dst, key)
		err := os.WriteFile(keyFile, dst, 0o600)
		require.NoError(t, err)

		_, err = NewEdSigner(FromFile(keyFile))
		require.ErrorContains(t, err, "invalid key size 65/64 for identity.key")
	})

	t.Run("valid key", func(t *testing.T) {
		ed, err := NewEdSigner()
		require.NoError(t, err)

		keyFile := filepath.Join(t.TempDir(), "identity.key")
		dst := make([]byte, hex.EncodedLen(len(ed.PrivateKey())))
		hex.Encode(dst, ed.PrivateKey())
		err = os.WriteFile(keyFile, dst, 0o600)
		require.NoError(t, err)

		ed2, err := NewEdSigner(FromFile(keyFile))
		require.NoError(t, err)
		require.Equal(t, ed.priv, ed2.priv)
		require.Equal(t, ed.PublicKey(), ed2.PublicKey())
		require.Equal(t, "identity.key", ed2.Name())
	})

	t.Run("fails if private key already set", func(t *testing.T) {
		ed, err := NewEdSigner()
		require.NoError(t, err)

		keyFile := filepath.Join(t.TempDir(), "identity.key")
		dst := make([]byte, hex.EncodedLen(len(ed.PrivateKey())))
		hex.Encode(dst, ed.PrivateKey())
		err = os.WriteFile(keyFile, dst, 0o600)
		require.NoError(t, err)

		_, err = NewEdSigner(WithPrivateKey(ed.PrivateKey()), FromFile(keyFile))
		require.ErrorContains(t, err, "invalid option FromFile: private key already set")
	})
}

func TestEdSigner_ToFile(t *testing.T) {
	t.Run("invalid file", func(t *testing.T) {
		_, err := NewEdSigner(ToFile("¿/ae_nonexistent"))
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.ErrorContains(t, err, "failed to write identity file")
	})

	t.Run("valid file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "identity.key")

		ed, err := NewEdSigner(ToFile(path))
		require.NoError(t, err)
		require.Equal(t, "identity.key", ed.Name())

		require.FileExists(t, path)
		data, err := os.ReadFile(path)
		require.NoError(t, err)

		key := make([]byte, hex.DecodedLen(len(data)))
		_, err = hex.Decode(key, data)
		require.NoError(t, err)

		require.Equal(t, []byte(ed.PrivateKey()), key)
	})

	t.Run("fails if file already set", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "identity.key")

		_, err := NewEdSigner(ToFile(path), ToFile(path))
		require.ErrorContains(t, err, "invalid option ToFile: file already set")
	})

	t.Run("valid if private key is passed", func(t *testing.T) {
		ed, err := NewEdSigner()
		require.NoError(t, err)

		_, err = NewEdSigner(WithPrivateKey(ed.PrivateKey()), ToFile(filepath.Join(t.TempDir(), "identity.key")))
		require.NoError(t, err)
	})

	t.Run("fails if file already exists", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "identity.key")

		_, err := NewEdSigner(ToFile(path))
		require.NoError(t, err)

		_, err = NewEdSigner(ToFile(path))
		require.ErrorIs(t, err, fs.ErrExist)
		require.ErrorContains(t, err, "save identity file identity.key")

		_, err = NewEdSigner(FromFile(path), ToFile(path))
		require.ErrorContains(t, err, "invalid option ToFile: file already set")
	})
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

func TestPublicKey_ShortString(t *testing.T) {
	pub := NewPublicKey([]byte{1, 2, 3})
	require.Equal(t, "010203", pub.String())
	require.Equal(t, "01020", pub.ShortString())

	pub = NewPublicKey([]byte{1, 2})
	require.Equal(t, pub.String(), pub.ShortString())
}
