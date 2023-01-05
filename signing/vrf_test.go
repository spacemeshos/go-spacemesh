package signing

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func Fuzz_VRFSignAndVerify(f *testing.F) {
	f.Fuzz(func(t *testing.T, message []byte, nonce uint64) {
		edSig, err := NewEdSigner()
		require.NoError(t, err, "failed to create EdSigner")

		vrfNonce := types.VRFPostIndex(nonce)

		vrfSig, err := edSig.VRFSigner(WithNonceForNode(vrfNonce, edSig.NodeID()))
		require.NoError(t, err, "failed to create VRF signer")

		signature, err := vrfSig.Sign(message)
		require.NoError(t, err, "failed to sign message")

		vrfVerify, err := NewVRFVerifier(WithNonceForNode(vrfNonce, edSig.NodeID()))
		require.NoError(t, err, "failed to create VRF verifier")

		ok := vrfVerify.Verify(edSig.NodeID(), message, signature)
		require.True(t, ok, "failed to verify VRF signature")
	})
}

func Test_VRFSignAndVerify(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(5)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	log := logtest.New(t).WithName("vrf")
	db := datastore.NewCachedDB(sql.InMemory(), log)

	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")

	nonce := types.VRFPostIndex(rand.Uint64())
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.NewLayerID(1),
				PrevATXID:  types.RandomATXID(),
			},
			NumUnits: 2,
			VRFNonce: &nonce,
		},
	}
	atx.Sig = signer.Sign(atx.SignedBytes())
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)

	atxs.Add(db, vAtx, time.Now())

	// Act & Assert
	vrfSig, err := signer.VRFSigner(WithNonceFromDB(db))
	require.NoError(t, err, "failed to create VRF signer")

	message := []byte("hello world")
	signature, err := vrfSig.Sign(message)
	require.NoError(t, err, "failed to sign message")

	vrfVerify, err := NewVRFVerifier(WithNonceFromDB(db))
	require.NoError(t, err, "failed to create VRF verifier")

	ok := vrfVerify.Verify(signer.NodeID(), message, signature)
	require.True(t, ok, "failed to verify VRF signature")
}

func Test_VRFSigner_NodeIDAndPublicKey(t *testing.T) {
	// Arrange
	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")

	// Act
	vrfSig, err := signer.VRFSigner(WithNonceForNode(1, signer.NodeID()))
	require.NoError(t, err, "failed to create VRF signer")

	// Assert
	require.Equal(t, signer.NodeID(), vrfSig.NodeID(), "VRF signer node ID does not match Ed signer node ID")
	require.Equal(t, signer.PublicKey(), vrfSig.PublicKey(), "VRF signer public key does not match Ed signer public key")
	require.Equal(t, types.BytesToNodeID(signer.PublicKey().Bytes()), vrfSig.NodeID(), "VRF signer node ID does not match Ed signer node ID")
}

func Test_VRFSigner_Validate(t *testing.T) {
	// Arrange
	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")

	// Act & Assert
	vrfSig, err := signer.VRFSigner()
	require.Nil(t, vrfSig, "VRF signer should be nil")
	require.EqualError(t, err, "no source for VRF nonces provided")
}

func Test_VRFSigner_WithNonceForNode(t *testing.T) {
	// Arrange
	signer, err := NewEdSigner()
	require.NoError(t, err, "failed to create EdSigner")
	vrfSig, err := signer.VRFSigner(WithNonceForNode(1, signer.NodeID()))
	require.NoError(t, err, "failed to create VRF signer")
	vrfVerify1, err := NewVRFVerifier(WithNonceForNode(1, signer.NodeID()))
	require.NoError(t, err, "failed to create VRF verifier")
	vrfVerify2, err := NewVRFVerifier(WithNonceForNode(2, signer.NodeID()))
	require.NoError(t, err, "failed to create VRF verifier")

	// Act & Assert
	sig, err := vrfSig.Sign([]byte("hello world"))
	require.NoError(t, err, "failed to sign message")

	ok := vrfVerify1.Verify(signer.NodeID(), []byte("hello world"), sig)
	require.True(t, ok, "failed to verify VRF signature")

	ok = vrfVerify2.Verify(signer.NodeID(), []byte("hello world"), sig)
	require.False(t, ok, "VRF signature should not be verified")
}
