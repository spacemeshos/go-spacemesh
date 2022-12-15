package signing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Fuzz_VRFSignAndVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, message []byte) {
		signer := NewEdSigner().VRFSigner()
		verifier := VRFVerifier{}

		t.Logf("message: %x\n", message)

		signature := signer.Sign(message)
		ok := verifier.Verify(signer.PublicKey(), message, signature)
		require.True(t, ok, "failed to verify VRF signature")
	})
}
