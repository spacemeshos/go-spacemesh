package signing

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func Fuzz_VRFSignAndVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, message []byte) {
		edSig, err := NewEdSigner()
		require.NoError(t, err, "failed to create EdSigner")

		vrfSig, err := edSig.VRFSigner()
		require.NoError(t, err, "failed to create VRF signer")

		signature := vrfSig.Sign(HARE, message)
		require.NoError(t, err, "failed to sign message")

		ok := VRFVerify(HARE, edSig.NodeID(), message, signature)
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
	signature := vrfSig.Sign(HARE, message)

	vrfVerify := NewVRFVerifier()
	ok := vrfVerify.Verify(HARE, signer.NodeID(), message, signature)
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
	sig := vrfSig.Sign(HARE, []byte("hello world"))

	ok := VRFVerify(HARE, signer.NodeID(), []byte("hello world"), sig)
	require.True(t, ok, "failed to verify VRF signature")

	ok = VRFVerify(HARE, signer.NodeID(), []byte("different message"), sig)
	require.False(t, ok, "VRF signature should not be verified")

	ok = VRFVerify(ATX, signer.NodeID(), []byte("hello world"), sig)
	require.False(t, ok, "VRF signature should not be verified")

	var sig2 types.VrfSignature
	copy(sig2[:], sig[:])
	sig2[0] = ^sig2[0] // flip all bits of first byte in the signature
	ok = VRFVerify(HARE, signer.NodeID(), []byte("hello world"), sig2)
	require.False(t, ok, "VRF signature should not be verified")
}

func Test_VRF_LSB_evenly_distributed(t *testing.T) {
	// Arrange
	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")
	vrfSig, err := signer.VRFSigner()
	require.NoError(t, err, "failed to create VRF signer")

	iterations := 10_000

	// Act
	var lsb [2]int
	for i := 0; i < iterations; i++ {
		msg := make([]byte, 32)
		_, err := rand.Read(msg)
		require.NoError(t, err, "failed to read random bytes")

		sig := vrfSig.Sign(HARE, msg)
		lsb[sig.LSB()&1]++
	}

	// Assert
	for i := 0; i < 2; i++ {
		maxDeviation := float64(250) // expecting at most a deviation of 5% from expected value (5_000)
		require.InDelta(t, iterations/2, lsb[i], maxDeviation, "LSB %d was not evenly distributed", i)
	}
}
