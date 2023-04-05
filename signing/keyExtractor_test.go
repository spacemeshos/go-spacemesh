package signing_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func TestPubKeyExtractor_WithPrefix(t *testing.T) {
	t.Run("same prefix", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		extractor, err := signing.NewPubKeyExtractor(signing.WithExtractorPrefix([]byte("one")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(signing.HARE, msg)

		pub, err := extractor.Extract(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.Equal(t, pub.Bytes(), signer.PublicKey().Bytes())

		nodeId, err := extractor.ExtractNodeID(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.Equal(t, nodeId, signer.NodeID())
	})

	t.Run("prefix mismatch", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		extractor, err := signing.NewPubKeyExtractor(signing.WithExtractorPrefix([]byte("two")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(signing.HARE, msg)

		pub, err := extractor.Extract(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.NotEqual(t, pub.Bytes(), signer.PublicKey().Bytes())

		nodeId, err := extractor.ExtractNodeID(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.NotEqual(t, nodeId, signer.NodeID())
	})

	t.Run("domain mismatch", func(t *testing.T) {
		signer, err := signing.NewEdSigner(signing.WithPrefix([]byte("one")))
		require.NoError(t, err)

		extractor, err := signing.NewPubKeyExtractor(signing.WithExtractorPrefix([]byte("one")))
		require.NoError(t, err)
		msg := []byte("test")
		sig := signer.Sign(signing.BALLOT, msg)

		pub, err := extractor.Extract(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.NotEqual(t, pub.Bytes(), signer.PublicKey().Bytes())

		nodeId, err := extractor.ExtractNodeID(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.NotEqual(t, nodeId, signer.NodeID())
	})
}

func Fuzz_PubKeyExtractor(f *testing.F) {
	f.Fuzz(func(t *testing.T, msg []byte, prefix []byte) {
		signer, err := signing.NewEdSigner(signing.WithPrefix(prefix))
		require.NoError(t, err)

		extractor, err := signing.NewPubKeyExtractor(signing.WithExtractorPrefix(prefix))
		require.NoError(t, err)

		sig := signer.Sign(signing.HARE, msg)

		pub, err := extractor.Extract(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.Equal(t, pub.Bytes(), signer.PublicKey().Bytes())
	})
}

func Fuzz_PubKeyExtractorNodeID(f *testing.F) {
	f.Fuzz(func(t *testing.T, msg []byte, prefix []byte) {
		signer, err := signing.NewEdSigner(signing.WithPrefix(prefix))
		require.NoError(t, err)

		extractor, err := signing.NewPubKeyExtractor(signing.WithExtractorPrefix(prefix))
		require.NoError(t, err)

		sig := signer.Sign(signing.HARE, msg)

		nodeID, err := extractor.ExtractNodeID(signing.HARE, msg, sig)
		require.NoError(t, err)
		require.Equal(t, nodeID, signer.NodeID())
	})
}
