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

func Test_DoubleMergeProof(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	setupMarriage := func(db sql.Executor) *ActivationTxV2 {
		wInitialAtx1 := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wInitialAtx1.Sign(sig)
		initialAtx1 := &types.ActivationTx{
			CommitmentATX: &wInitialAtx1.Initial.CommitmentATX,
		}
		initialAtx1.SetID(wInitialAtx1.ID())
		initialAtx1.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, initialAtx1, wInitialAtx1.Blob()))

		wInitialAtx2 := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wInitialAtx2.Sign(otherSig)
		initialAtx2 := &types.ActivationTx{}
		initialAtx2.SetID(wInitialAtx2.ID())
		initialAtx2.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, initialAtx2, wInitialAtx2.Blob()))

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(marrySig, types.EmptyATXID, marrySig.NodeID()),
			withMarriageCertificate(sig, wInitialAtx1.ID(), marrySig.NodeID()),
			withMarriageCertificate(otherSig, wInitialAtx2.ID(), marrySig.NodeID()),
		)
		wMarriageAtx.Sign(marrySig)

		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = marrySig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))
		return wMarriageAtx
	}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		marriageAtx := setupMarriage(db)

		atx1 := newActivationTxV2(
			withMarriageATX(marriageAtx.ID()),
			withPublishEpoch(marriageAtx.PublishEpoch+1),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageATX(marriageAtx.ID()),
			withPublishEpoch(marriageAtx.PublishEpoch+1),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMergeProof(db, atx1, atx2)
		require.NoError(t, err)
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("same ATX ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		marriageAtx := setupMarriage(db)

		atx1 := newActivationTxV2(
			withMarriageATX(marriageAtx.ID()),
			withPublishEpoch(marriageAtx.PublishEpoch+1),
		)
		atx1.Sign(sig)

		proof, err := NewDoubleMergeProof(db, atx1, atx1)
		require.EqualError(t, err, "ATXs have the same ID")
		require.Nil(t, proof)

		proof = &ProofDoubleMerge{
			ATXID1: atx1.ID(),
			ATXID2: atx1.ID(),
		}
		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATXs have the same ID")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("ATXs must have different signers", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx1 := newActivationTxV2()
		atx1.Sign(sig)

		atx2 := newActivationTxV2()
		atx2.Sign(sig)

		proof, err := NewDoubleMergeProof(db, atx1, atx2)
		require.ErrorContains(t, err, "ATXs have the same smesher")
		require.Nil(t, proof)
	})

	t.Run("ATXs must be published in the same epoch", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx := newActivationTxV2(
			withPublishEpoch(1),
		)
		atx.Sign(sig)

		atx2 := newActivationTxV2(
			withPublishEpoch(2),
		)
		atx2.Sign(otherSig)
		proof, err := NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATXs have different publish epoch")
		require.Nil(t, proof)
	})

	t.Run("ATXs must have valid marriage ATX", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		atx := newActivationTxV2(
			withPublishEpoch(1),
		)
		atx.Sign(sig)

		atx2 := newActivationTxV2(
			withPublishEpoch(1),
		)
		atx2.Sign(otherSig)

		// ATX 1 has no marriage
		proof, err := NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATX 1 have no marriage ATX")
		require.Nil(t, proof)

		// ATX 2 has no marriage
		atx.MarriageATX = new(types.ATXID)
		*atx.MarriageATX = types.RandomATXID()
		proof, err = NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATX 2 have no marriage ATX")
		require.Nil(t, proof)

		// ATX 1 and 2 must have the same marriage ATX
		atx2.MarriageATX = new(types.ATXID)
		*atx2.MarriageATX = types.RandomATXID()
		proof, err = NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATXs have different marriage ATXs")
		require.Nil(t, proof)

		// Marriage ATX must be valid
		atx2.MarriageATX = atx.MarriageATX
		proof, err = NewDoubleMergeProof(db, atx, atx2)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, proof)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		marriageAtx := setupMarriage(db)

		atx1 := newActivationTxV2(
			withMarriageATX(marriageAtx.ID()),
			withPublishEpoch(marriageAtx.PublishEpoch+1),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageATX(marriageAtx.ID()),
			withPublishEpoch(marriageAtx.PublishEpoch+1),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMergeProof(db, atx1, atx2)
		require.NoError(t, err)

		// invalid marriage ATX ID
		marriageAtxID := proof.MarriageATX
		proof.MarriageATX = types.RandomATXID()
		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid marriage ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.MarriageATX = marriageAtxID

		// invalid marriage ATX smesher ID
		smesherID := proof.MarriageATXSmesherID
		proof.MarriageATXSmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid marriage ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.MarriageATXSmesherID = smesherID

		// invalid ATX1 ID
		id1 := proof.ATXID1
		proof.ATXID1 = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXID1 = id1

		// invalid ATX2 ID
		id2 := proof.ATXID2
		proof.ATXID2 = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 2 invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXID2 = id2

		// invalid ATX1 smesher ID
		smesherID1 := proof.SmesherID1
		proof.SmesherID1 = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.SmesherID1 = smesherID1

		// invalid ATX2 smesher ID
		smesherID2 := proof.SmesherID2
		proof.SmesherID2 = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 2 invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.SmesherID2 = smesherID2

		// invalid ATX1 signature
		signature1 := proof.Signature1
		proof.Signature1 = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Signature1 = signature1

		// invalid ATX2 signature
		signature2 := proof.Signature2
		proof.Signature2 = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 2 invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Signature2 = signature2

		// invalid publish epoch proof 1
		hash := proof.PublishEpochProof1[0]
		proof.PublishEpochProof1[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid publish epoch proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.PublishEpochProof1[0] = hash

		// invalid publish epoch proof 2
		hash = proof.PublishEpochProof2[0]
		proof.PublishEpochProof2[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 2 invalid publish epoch proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.PublishEpochProof2[0] = hash

		// invalid marriage ATX proof 1
		hash = proof.MarriageATXProof1[0]
		proof.MarriageATXProof1[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid marriage ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.MarriageATXProof1[0] = hash

		// invalid marriage ATX proof 2
		hash = proof.MarriageATXProof2[0]
		proof.MarriageATXProof2[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 2 invalid marriage ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.MarriageATXProof2[0] = hash
	})
}
