package signing

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNewVRFSigner(t *testing.T) {
	a := assert.New(t)

	signer, pubkey, err := NewVRFSigner(nil)
	a.Nil(signer)
	a.Nil(pubkey)
	a.EqualError(err, "seed must be >=32 bytes (len(seed)=0)")

	signer, pubkey, err = NewVRFSigner([]byte("too short"))
	a.Nil(signer)
	a.Nil(pubkey)
	a.EqualError(err, "seed must be >=32 bytes (len(seed)=9)")

	signer, pubkey, err = NewVRFSigner([]byte("this seed is 32 bytes long: good"))
	a.NotNil(signer)
	a.NotNil(pubkey)
	a.NoError(err)

	signer, pubkey, err = NewVRFSigner(make([]byte, 32)) // all zeros works
	a.NotNil(signer)
	a.NotNil(pubkey)
	a.NoError(err)

	signer, pubkey, err = NewVRFSigner([]byte("this seed is longer than 32 bytes, which also works"))
	a.NotNil(signer)
	a.NotNil(pubkey)
	a.NoError(err)
}

func TestVRFSigner_Sign_Verify(t *testing.T) {
	a := assert.New(t)

	signer, pubkey, err := NewVRFSigner(make([]byte, 32))
	a.NotNil(signer)
	a.NotNil(pubkey)
	a.NoError(err)

	m := []byte("this is just some message")
	sig := signer.Sign(m)
	a.NotNil(sig)
	a.Len(sig, 64)

	valid := VRFVerify(pubkey, m, sig)
	a.True(valid)

	valid = VRFVerify(flipFirstBit(pubkey), m, sig)
	a.False(valid)

	valid = VRFVerify(pubkey, flipFirstBit(m), sig)
	a.False(valid)

	valid = VRFVerify(pubkey, m, flipFirstBit(sig))
	a.False(valid)

	valid = VRFVerify(pubkey, m, []byte{})
	a.False(valid)

	// Public key must be 32 bytes long
	a.PanicsWithValue("ed25519: bad public key length: 31", func() {
		VRFVerify(pubkey[:31], m, sig)
	})
}

func flipFirstBit(b []byte) []byte {
	flipped := make([]byte, len(b))
	copy(flipped, b)
	flipped[0] = flipped[0] ^ 1
	return flipped
}
