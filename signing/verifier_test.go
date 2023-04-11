package signing_test

import (
	"crypto/ed25519"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestEdVerifier_Verify(t *testing.T) {
	m := make([]byte, 4)
	rand.Read(m)
	signed := make([]byte, len(m)+1)
	signed[0] = byte(signing.ATX)
	copy(signed[1:], m)

	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	var sig types.EdSignature
	copy(sig[:], ed25519.Sign(priv, signed))

	ed, err := signing.NewEdVerifier()
	require.NoError(t, err)

	ok := ed.Verify(signing.ATX, types.BytesToNodeID(pub), m, sig)
	require.Truef(t, ok, "failed to verify message %x with sig %x", m, sig)
}

func TestVerifier_WithPrefix(t *testing.T) {
	t.Run("same prefix", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		verifier, err := signing.NewEdVerifier(signing.WithVerifierPrefix([]byte("one")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(signing.ATX, msg)

		ok := verifier.Verify(signing.ATX, signer.NodeID(), msg, sig)
		require.True(t, ok)
	})

	t.Run("prefix mismatch", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		verifier, err := signing.NewEdVerifier(signing.WithVerifierPrefix([]byte("two")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(signing.ATX, msg)

		ok := verifier.Verify(signing.ATX, signer.NodeID(), msg, sig)
		require.False(t, ok)
	})

	t.Run("domain mismatch", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		verifier, err := signing.NewEdVerifier(signing.WithVerifierPrefix([]byte("one")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(signing.ATX, msg)

		// Verification panics if the domain is not supported by the verifier.
		require.Panics(t, func() { verifier.Verify(signing.HARE, signer.NodeID(), msg, sig) })
	})
}

func Fuzz_EdVerifier(f *testing.F) {
	f.Fuzz(func(t *testing.T, msg []byte, prefix []byte) {
		signer, err := signing.NewEdSigner(signing.WithPrefix(prefix))
		require.NoError(t, err)

		verifier, err := signing.NewEdVerifier(signing.WithVerifierPrefix(prefix))
		require.NoError(t, err)

		sig := signer.Sign(signing.ATX, msg)

		ok := verifier.Verify(signing.ATX, signer.NodeID(), msg, sig)
		require.True(t, ok)
	})
}
