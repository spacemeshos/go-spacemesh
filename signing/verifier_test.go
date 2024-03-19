package signing_test

import (
	"crypto/rand"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
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

	ok := signing.NewEdVerifier().Verify(signing.ATX, types.BytesToNodeID(pub), m, sig)
	require.Truef(t, ok, "failed to verify message %x with sig %x", m, sig)
}

func TestVerifier_WithPrefix(t *testing.T) {
	t.Run("same prefix", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		verifier := signing.NewEdVerifier(signing.WithVerifierPrefix([]byte("one")))
		msg := []byte("test")
		sig := signer.Sign(signing.ATX, msg)

		ok := verifier.Verify(signing.ATX, signer.NodeID(), msg, sig)
		require.True(t, ok)
	})

	t.Run("prefix mismatch", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		verifier := signing.NewEdVerifier(signing.WithVerifierPrefix([]byte("two")))
		msg := []byte("test")
		sig := signer.Sign(signing.ATX, msg)

		ok := verifier.Verify(signing.ATX, signer.NodeID(), msg, sig)
		require.False(t, ok)
	})

	t.Run("domain mismatch", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		verifier := signing.NewEdVerifier(signing.WithVerifierPrefix([]byte("one")))
		msg := []byte("test")
		sig := signer.Sign(signing.ATX, msg)

		// Verification panics if the domain is not supported by the verifier.
		require.False(t, verifier.Verify(signing.HARE, signer.NodeID(), msg, sig))
	})
}

func Fuzz_EdVerifier(f *testing.F) {
	f.Fuzz(func(t *testing.T, msg, prefix []byte) {
		signer, err := signing.NewEdSigner(signing.WithPrefix(prefix))
		require.NoError(t, err)

		verifier := signing.NewEdVerifier(signing.WithVerifierPrefix(prefix))

		sig := signer.Sign(signing.ATX, msg)

		ok := verifier.Verify(signing.ATX, signer.NodeID(), msg, sig)
		require.True(t, ok)
	})
}
