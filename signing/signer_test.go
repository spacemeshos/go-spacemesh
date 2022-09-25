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
	_, err := NewEdSignerFromBuffer(b)
	assert.NotNil(t, err)
	assert.Equal(t, "buffer too small", err.Error())
	b = make([]byte, 64)
	_, err = NewEdSignerFromBuffer(b)
	assert.NotNil(t, err)
	assert.Equal(t, "private and public does not match", err.Error())
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
	assert.Equal(t, []byte(ed.pubKey), []byte(ed.privKey[32:]))
}

func TestEdSigner_ToBuffer(t *testing.T) {
	ed := NewEdSigner()
	buff := ed.ToBuffer()
	ed2, err := NewEdSignerFromBuffer(buff)
	assert.Nil(t, err)
	assert.Equal(t, ed.privKey, ed2.privKey)
	assert.Equal(t, ed.pubKey, ed2.pubKey)
}

func TestPublicKey_ShortString(t *testing.T) {
	pub := NewPublicKey([]byte{1, 2, 3})
	assert.Equal(t, "010203", pub.String())
	assert.Equal(t, "01020", pub.ShortString())

	pub = NewPublicKey([]byte{1, 2})
	assert.Equal(t, pub.String(), pub.ShortString())
}

func TestGenesisEdVerifier_Verify(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	genesisID := [20]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	genesisEdSigner := NewGenesisEdSigner(genesisID)
	verifier := NewGenesisEdVerifier(genesisID)

	t.Run("Fail - short msg", func(t *testing.T) {
		t.Parallel()

		msg := make([]byte, 19)
		rand.Read(msg)
		sign := genesisEdSigner.Sign(msg)

		r.Equal(false, verifier.Verify(genesisEdSigner.signer.PublicKey(), msg, sign))
	})

	t.Run("Fail - wrong msg", func(t *testing.T) {
		t.Parallel()

		msg := make([]byte, 20)
		rand.Read(msg)
		sign := genesisEdSigner.Sign(msg)
		msg[0] = 2

		r.Equal(false, verifier.Verify(genesisEdSigner.signer.PublicKey(), append(genesisID[:], msg...), sign))
	})

	t.Run("Fail - wrong sign", func(t *testing.T) {
		t.Parallel()

		msg := make([]byte, 20)
		rand.Read(msg)
		sign := genesisEdSigner.Sign(msg)[1:]

		r.Equal(false, verifier.Verify(genesisEdSigner.signer.PublicKey(), append(genesisID[:], msg...), sign))
	})

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		msg := make([]byte, 30)
		rand.Read(msg)
		sign := genesisEdSigner.Sign(msg)

		r.Equal(true, verifier.Verify(genesisEdSigner.signer.PublicKey(), append(genesisID[:], msg...), sign))
	})
}
