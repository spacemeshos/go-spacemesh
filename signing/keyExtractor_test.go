package signing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPubKeyExtractor_WithPrefix(t *testing.T) {
	t.Run("same prefix", func(t *testing.T) {
		signer, err := NewEdSigner(WithPrefix([]byte("one")))
		require.NoError(t, err)

		extractor, err := NewPubKeyExtractor(WithExtractorPrefix([]byte("one")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(msg)

		pub, err := extractor.Extract(msg, sig)
		require.NoError(t, err)
		require.Equal(t, pub.Bytes(), signer.PublicKey().Bytes())

		nodeId, err := extractor.ExtractNodeID(msg, sig)
		require.NoError(t, err)
		require.Equal(t, nodeId, signer.NodeID())
	})

	t.Run("prefix mismatch", func(t *testing.T) {
		signer, err := NewEdSigner(WithPrefix([]byte("one")))
		require.NoError(t, err)

		extractor, err := NewPubKeyExtractor(WithExtractorPrefix([]byte("two")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(msg)

		pub, err := extractor.Extract(msg, sig)
		require.NoError(t, err)
		require.NotEqual(t, pub.Bytes(), signer.PublicKey().Bytes())

		nodeId, err := extractor.ExtractNodeID(msg, sig)
		require.NoError(t, err)
		require.NotEqual(t, nodeId, signer.NodeID())
	})
}

func Fuzz_PubKeyExtractor(f *testing.F) {
	f.Fuzz(func(t *testing.T, msg []byte, prefix []byte) {
		signer, err := NewEdSigner(WithPrefix(prefix))
		require.NoError(t, err)

		extractor, err := NewPubKeyExtractor(WithExtractorPrefix(prefix))
		require.NoError(t, err)

		sig := signer.Sign(msg)

		pub, err := extractor.Extract(msg, sig)
		require.NoError(t, err)
		require.Equal(t, pub.Bytes(), signer.PublicKey().Bytes())
	})
}
