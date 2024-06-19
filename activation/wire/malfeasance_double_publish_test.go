package wire

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func Test_DoublePublishProof(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof, err := NewDoublePublishProof(atx1, atx2)
		require.NoError(t, err)

		verifier := signing.NewEdVerifier()
		require.NoError(t, proof.Valid(verifier))
	})

	t.Run("not same epoch", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(11))
		atx2.Sign(sig)

		proof, err := NewDoublePublishProof(atx1, atx2)
		require.ErrorContains(t, err, "ATXs have different publish epochs")
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		proof = &ProofDoublePublish{
			Proofs: [2]PublishProof{
				{
					ATXID:     atx1.ID(),
					PubEpoch:  atx1.PublishEpoch,
					Proof:     proof1,
					SmesherID: atx1.SmesherID,
					Signature: atx1.Signature,
				},
				{
					ATXID:     atx2.ID(),
					PubEpoch:  atx2.PublishEpoch,
					Proof:     proof2,
					SmesherID: atx2.SmesherID,
					Signature: atx2.Signature,
				},
			},
		}

		verifier := signing.NewEdVerifier()
		err = proof.Valid(verifier)
		require.ErrorContains(t, err, "different publish epochs")
	})

	t.Run("not same smesher", func(t *testing.T) {
		sig1 := sig
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig1)

		sig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig2)

		proof, err := NewDoublePublishProof(atx1, atx2)
		require.ErrorContains(t, err, "ATXs have different smesher IDs")
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		proof = &ProofDoublePublish{
			Proofs: [2]PublishProof{
				{
					ATXID:     atx1.ID(),
					PubEpoch:  atx1.PublishEpoch,
					Proof:     proof1,
					SmesherID: atx1.SmesherID,
					Signature: atx1.Signature,
				},
				{
					ATXID:     atx2.ID(),
					PubEpoch:  atx2.PublishEpoch,
					Proof:     proof2,
					SmesherID: atx2.SmesherID,
					Signature: atx2.Signature,
				},
			},
		}

		verifier := signing.NewEdVerifier()
		err = proof.Valid(verifier)
		require.ErrorContains(t, err, "different smesher IDs")
	})

	t.Run("same ATX ID", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		proof, err := NewDoublePublishProof(atx1, atx1)
		require.ErrorContains(t, err, "ATXs have the same ID")
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof = &ProofDoublePublish{
			Proofs: [2]PublishProof{
				{
					ATXID:     atx1.ID(),
					PubEpoch:  atx1.PublishEpoch,
					Proof:     proof1,
					SmesherID: atx1.SmesherID,
					Signature: atx1.Signature,
				},
				{
					ATXID:     atx1.ID(),
					PubEpoch:  atx1.PublishEpoch,
					Proof:     proof1,
					SmesherID: atx1.SmesherID,
					Signature: atx1.Signature,
				},
			},
		}

		verifier := signing.NewEdVerifier()
		err = proof.Valid(verifier)
		require.ErrorContains(t, err, "same ATX ID")
	})

	t.Run("invalid proof", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		// manually construct an invalid proof
		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		proof := &ProofDoublePublish{
			Proofs: [2]PublishProof{
				{
					ATXID:     atx1.ID(),
					PubEpoch:  atx1.PublishEpoch,
					Proof:     slices.Clone(proof1),
					SmesherID: atx1.SmesherID,
					Signature: atx1.Signature,
				},
				{
					ATXID:     atx2.ID(),
					PubEpoch:  atx2.PublishEpoch,
					Proof:     slices.Clone(proof2),
					SmesherID: atx2.SmesherID,
					Signature: atx2.Signature,
				},
			},
		}

		verifier := signing.NewEdVerifier()
		proof.Proofs[0].Proof[0] = types.RandomHash()
		err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid publish epoch proof")

		proof.Proofs[0].Proof[0] = proof1[0]
		proof.Proofs[1].Proof[0] = types.RandomHash()
		err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid publish epoch proof")
	})

	t.Run("invalid signature", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof, err := NewDoublePublishProof(atx1, atx2)
		require.NoError(t, err)

		verifier := signing.NewEdVerifier()

		proof.Proofs[0].Signature = types.RandomEdSignature()
		err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid signature")

		proof.Proofs[0].Signature = atx1.Signature
		proof.Proofs[1].Signature = types.RandomEdSignature()
		err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid signature")
	})
}
