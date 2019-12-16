package signing

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"testing"
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
	log.Info("pub: %v priv: %x", ed.PublicKey().String(), ed.privKey)
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
