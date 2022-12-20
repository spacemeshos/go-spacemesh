package signing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Fuzz_VRFSignAndVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, message []byte, nonce uint64) {
		edSig, err := NewEdSigner()
		require.NoError(t, err, "failed to create EdSigner")

		vrfSig, err := edSig.VRFSigner(
			WithVRFNonce(types.VRFPostIndex(nonce)),
		)
		require.NoError(t, err, "failed to create VRF signer")

		verifier := VRFVerifier{}

		t.Logf("message: %x\n", message)

		signature := vrfSig.Sign(message)
		ok := verifier.Verify(vrfSig.PublicKey(), message, signature)
		require.True(t, ok, "failed to verify VRF signature")
	})
}
