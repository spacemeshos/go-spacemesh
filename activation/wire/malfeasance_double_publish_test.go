package wire

import (
	"math/rand/v2"
	"testing"

	"github.com/spacemeshos/merkle-tree"
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

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		doublePublishProof := &ProofDoublePublish{
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
		ok, err := doublePublishProof.Valid(verifier)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("not same epoch", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(11))
		atx2.Sign(sig)

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		doublePublishProof := &ProofDoublePublish{
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
		ok, err := doublePublishProof.Valid(verifier)
		require.ErrorContains(t, err, "different publish epochs")
		require.False(t, ok)
	})

	t.Run("not same smesher", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		doublePublishProof := &ProofDoublePublish{
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
					SmesherID: types.RandomNodeID(),
					Signature: atx2.Signature,
				},
			},
		}

		verifier := signing.NewEdVerifier()
		ok, err := doublePublishProof.Valid(verifier)
		require.ErrorContains(t, err, "different smesher IDs")
		require.False(t, ok)
	})

	t.Run("same ATX ID", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		doublePublishProof := &ProofDoublePublish{
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
		ok, err := doublePublishProof.Valid(verifier)
		require.ErrorContains(t, err, "same ATX ID")
		require.False(t, ok)
	})

	t.Run("invalid proof 1", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)
		proof1[0] = types.RandomHash()

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		doublePublishProof := &ProofDoublePublish{
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
		ok, err := doublePublishProof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid")
		require.False(t, ok)
	})

	t.Run("invalid proof 2", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)
		proof2[0] = types.RandomHash()

		doublePublishProof := &ProofDoublePublish{
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
		ok, err := doublePublishProof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid")
		require.False(t, ok)
	})

	t.Run("invalid signature 1", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		doublePublishProof := &ProofDoublePublish{
			Proofs: [2]PublishProof{
				{
					ATXID:     atx1.ID(),
					PubEpoch:  atx1.PublishEpoch,
					Proof:     proof1,
					SmesherID: atx1.SmesherID,
					Signature: types.RandomEdSignature(),
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
		ok, err := doublePublishProof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid signature")
		require.False(t, ok)
	})

	t.Run("invalid signature 2", func(t *testing.T) {
		atx1 := newActivationTxV2(WithPublishEpoch(10))
		atx1.Sign(sig)

		atx2 := newActivationTxV2(WithPublishEpoch(10))
		atx2.Sign(sig)

		proof1, err := publishEpochProof(atx1)
		require.NoError(t, err)

		proof2, err := publishEpochProof(atx2)
		require.NoError(t, err)

		doublePublishProof := &ProofDoublePublish{
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
					Signature: types.RandomEdSignature(),
				},
			},
		}

		verifier := signing.NewEdVerifier()
		ok, err := doublePublishProof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid signature")
		require.False(t, ok)
	})
}

func publishEpochProof(atx *ActivationTxV2) ([]types.Hash32, error) {
	tree, err := merkle.NewTreeBuilder().
		WithLeavesToProve(map[uint64]bool{uint64(PublishEpochIndex): true}).
		WithHashFunc(atxTreeHash).
		Build()
	if err != nil {
		return nil, err
	}
	atx.merkleTree(tree)
	proof := tree.Proof()

	proofHashes := make([]types.Hash32, len(proof))
	for i, h := range proof {
		proofHashes[i] = types.Hash32(h)
	}

	return proofHashes, nil
}
