package signing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Fuzz_VRFSignAndVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, message []byte, nonce uint64, epoch uint64) {
		edSig, err := NewEdSigner()
		require.NoError(t, err, "failed to create EdSigner")

		vrfSig, err := edSig.VRFSigner()
		require.NoError(t, err, "failed to create VRF signer")

		signature := vrfSig.Sign(message)
		require.NoError(t, err, "failed to sign message")

		ok := VRFVerify(edSig.NodeID(), message, signature)
		require.True(t, ok, "failed to verify VRF signature")
	})
}

func Test_VRFSignAndVerify(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(5)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")

	// Act & Assert
	vrfSig, err := signer.VRFSigner()
	require.NoError(t, err, "failed to create VRF signer")

	message := []byte("hello world")
	signature := vrfSig.Sign(message)

	vrfVerify := NewVRFVerifier()
	ok := vrfVerify.Verify(signer.NodeID(), message, signature)
	require.True(t, ok, "failed to verify VRF signature")
}

func Test_VRFSigner_NodeIDAndPublicKey(t *testing.T) {
	// Arrange
	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")

	// Act
	vrfSig, err := signer.VRFSigner()
	require.NoError(t, err, "failed to create VRF signer")

	// Assert
	require.Equal(t, signer.NodeID(), vrfSig.NodeID(), "VRF signer node ID does not match Ed signer node ID")
	require.Equal(t, signer.PublicKey(), vrfSig.PublicKey(), "VRF signer public key does not match Ed signer public key")
	require.Equal(t, types.BytesToNodeID(signer.PublicKey().Bytes()), vrfSig.NodeID(), "VRF signer node ID does not match Ed signer node ID")
}

func Test_VRFVerifier(t *testing.T) {
	// Arrange
	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")
	vrfSig, err := signer.VRFSigner()
	require.NoError(t, err, "failed to create VRF signer")

	// Act & Assert
	sig := vrfSig.Sign([]byte("hello world"))

	ok := VRFVerify(signer.NodeID(), []byte("hello world"), sig)
	require.True(t, ok, "failed to verify VRF signature")

	ok = VRFVerify(signer.NodeID(), []byte("different message"), sig)
	require.False(t, ok, "VRF signature should not be verified")

	sig2 := make([]byte, len(sig))
	copy(sig2, sig)
	sig2[0] = ^sig2[0] // flip all bits of first byte in the signature
	ok = VRFVerify(signer.NodeID(), []byte("hello world"), sig2)
	require.False(t, ok, "VRF signature should not be verified")
}
