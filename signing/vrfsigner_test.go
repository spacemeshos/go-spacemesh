package signing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Fuzz_VRFSignAndVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, nonce uint64, message []byte) {
		signer := NewEdSigner().VRFSigner(nonce)
		verifier := VRFVerifier{}

		t.Logf("nonce: %d\n", nonce)
		t.Logf("message: %x\n", message)

		signature := signer.Sign(message)
		ok := verifier.Verify(signer.PublicKey(), nonce, message, signature)
		require.True(t, ok, "failed to verify VRF signature")
	})
}
