package wire

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type testAtxV2Opt func(*ActivationTxV2)

func WithPublishEpoch(epoch types.EpochID) testAtxV2Opt {
	return func(atx *ActivationTxV2) {
		atx.PublishEpoch = epoch
	}
}

func newActivationTxV2(opts ...testAtxV2Opt) *ActivationTxV2 {
	atx := &ActivationTxV2{
		PublishEpoch:   rand.N(types.EpochID(255)),
		PositioningATX: types.RandomATXID(),
		PreviousATXs:   make([]types.ATXID, 1+rand.IntN(255)),
		NiPosts: []NiPostsV2{
			{
				Membership: MerkleProofV2{
					Nodes:       make([]types.Hash32, 32),
					LeafIndices: make([]uint64, 256),
				},
				Challenge: types.RandomHash(),
				Posts: []SubPostV2{
					{
						MarriageIndex: rand.Uint32N(256),
						PrevATXIndex:  0,
						Post: PostV1{
							Nonce:   0,
							Indices: make([]byte, 800),
							Pow:     0,
						},
					},
				},
			},
		},
	}
	for _, opt := range opts {
		opt(atx)
	}
	return atx
}

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
		ok, err := proof.Valid(verifier)
		require.NoError(t, err)
		require.True(t, ok)
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
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "different publish epochs")
		require.False(t, ok)
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
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "different smesher IDs")
		require.False(t, ok)
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
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "same ATX ID")
		require.False(t, ok)
	})

	t.Run("invalid proof 1", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		// manually construct an invalid proof
		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)
		proof1[0] = types.RandomHash()

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		proof := &ProofDoublePublish{
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
		ok, err := proof.Valid(verifier)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("invalid proof 2", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		// manually construct an invalid proof
		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)
		proof2[0] = types.RandomHash()

		proof := &ProofDoublePublish{
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
		ok, err := proof.Valid(verifier)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("invalid signature 1", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof, err := NewDoublePublishProof(atx1, atx2)
		require.NoError(t, err)

		proof.Proofs[0].Signature = types.RandomEdSignature()

		verifier := signing.NewEdVerifier()
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid signature")
		require.False(t, ok)
	})

	t.Run("invalid signature 2", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof, err := NewDoublePublishProof(atx1, atx2)
		require.NoError(t, err)

		proof.Proofs[1].Signature = types.RandomEdSignature()

		verifier := signing.NewEdVerifier()
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid signature")
		require.False(t, ok)
	})
}
