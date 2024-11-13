package wire

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_InvalidPrevAtxProofV2(t *testing.T) {
	// sig is the identity that creates the ATXs referencing the same prevATX
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// pubSig is the identity that publishes a merged ATX with the same prevATX
	pubSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// marrySig is the identity that publishes the marriage ATX
	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	newMergedATXv2 := func(
		db sql.Executor,
		prevATX types.ATXID,
	) *ActivationTxV2 {
		wInitialAtx := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wInitialAtx.Sign(sig)
		initialAtx := &types.ActivationTx{
			CommitmentATX: &wInitialAtx.Initial.CommitmentATX,
		}
		initialAtx.SetID(wInitialAtx.ID())
		initialAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, initialAtx, wInitialAtx.Blob()))

		wPubInitialAtx := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wPubInitialAtx.Sign(pubSig)
		pubInitialAtx := &types.ActivationTx{}
		pubInitialAtx.SetID(wPubInitialAtx.ID())
		pubInitialAtx.SmesherID = pubSig.NodeID()
		require.NoError(t, atxs.Add(db, pubInitialAtx, wPubInitialAtx.Blob()))

		marryInitialAtx := types.RandomATXID()

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(marrySig, types.EmptyATXID, marrySig.NodeID()),
			withMarriageCertificate(sig, wInitialAtx.ID(), marrySig.NodeID()),
			withMarriageCertificate(pubSig, wPubInitialAtx.ID(), marrySig.NodeID()),
		)
		wMarriageAtx.Sign(marrySig)

		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = marrySig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withPreviousATXs(marryInitialAtx, wPubInitialAtx.ID(), prevATX),
			withMarriageATX(wMarriageAtx.ID()),
			withNIPost(
				withNIPostMembershipProof(MerkleProofV2{}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 0,
					PrevATXIndex:  0,
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 1,
					PrevATXIndex:  2,
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 2,
					PrevATXIndex:  1,
				}),
			),
		)
		atx.Sign(pubSig)
		return atx
	}

	t.Run("valid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(7),
		)
		atx2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// verify the proof
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("valid merged & solo atx", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		prevAtx := &types.ActivationTx{}
		prevAtx.SetID(prevATXID)
		prevAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, prevAtx, types.AtxBlob{}))
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newMergedATXv2(db, prevATXID)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// verify the proof
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	// valid merged & merged is covered by either double marry or double merge proofs

	t.Run("same ATX ID", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		atx1 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
		)
		atx1.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx1, sig.NodeID())
		require.ErrorContains(t, err, "ATXs have the same ID")
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof = &ProofInvalidPrevAtxV2{
			NodeID:  sig.NodeID(),
			PrevATX: atx1.PreviousATXs[0],

			Proofs: [2]InvalidPrevAtxProof{
				{
					ATXID: atx1.ID(),
				},
				{
					ATXID: atx1.ID(),
				},
			},
		}

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)

		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "same ATX ID")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("smesher ID mismatch", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATX),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(prevATX),
		)
		atx2.Sign(pubSig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.EqualError(t, err, "ATX2 is not a merged ATX, but NodeID is different from SmesherID")
		require.Nil(t, proof)

		proof, err = NewInvalidPrevAtxProofV2(db, atx1, atx2, pubSig.NodeID())
		require.EqualError(t, err, "ATX1 is not a merged ATX, but NodeID is different from SmesherID")
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof1, err := createInvalidPrevAtxProof(atx1, atx1.PreviousATXs[0], 0, 0, nil)
		require.NoError(t, err)

		proof2, err := createInvalidPrevAtxProof(atx2, atx2.PreviousATXs[0], 0, 0, nil)
		require.NoError(t, err)

		proof = &ProofInvalidPrevAtxV2{
			NodeID:  sig.NodeID(),
			PrevATX: atx1.PreviousATXs[0],

			Proofs: [2]InvalidPrevAtxProof{
				proof1, proof2,
			},
		}

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "proof 2 is invalid: missing marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid ATX signature", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		atx1 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
		)
		atx2.Sign(sig)

		atx2.Signature = types.RandomEdSignature()
		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.EqualError(t, err, "ATX2 has an invalid signature")
		require.Nil(t, proof)
	})

	t.Run("prev ATX has not been reused", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
			withPublishEpoch(7),
		)
		atx2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.EqualError(t, err, "ATXs reference different previous ATXs")
		require.Nil(t, proof)
	})
}

func Test_InvalidPrevAtxProofV1(t *testing.T) {
	// sig is the identity that creates the ATXs referencing the same prevATX
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// pubSig is the identity that publishes a merged ATX with the same prevATX
	// pubSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// marrySig is the identity that publishes the marriage ATX
	// marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	t.Run("valid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(sig)

		atxv2 := newActivationTxV2(
			withPreviousATXs(prevATX),
			withPublishEpoch(7),
		)
		atxv2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// verify the proof
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	// implement more tests here
}
