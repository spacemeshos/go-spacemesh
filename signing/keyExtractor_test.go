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
	})
}

// TODO(mafa): this fails after a very short fuzzing period.
// This seems to be caused when the prefix is the same as the start of the message.
// E.g. msg is 0x7fe805 and prefix is 0x7fe8.
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
