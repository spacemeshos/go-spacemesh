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

func Test_NewDoubleMergeProof(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	setupMarriage := func(db sql.Executor) *ActivationTxV2 {
		wInitialAtx1 := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wInitialAtx1.Sign(signer1)
		initialAtx1 := &types.ActivationTx{
			CommitmentATX: &wInitialAtx1.Initial.CommitmentATX,
		}
		initialAtx1.SetID(wInitialAtx1.ID())
		initialAtx1.SmesherID = signer1.NodeID()
		require.NoError(t, atxs.Add(db, initialAtx1, wInitialAtx1.Blob()))

		wInitialAtx2 := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wInitialAtx2.Sign(signer2)
		initialAtx2 := &types.ActivationTx{}
		initialAtx2.SetID(wInitialAtx2.ID())
		initialAtx2.SmesherID = signer2.NodeID()
		require.NoError(t, atxs.Add(db, initialAtx2, wInitialAtx2.Blob()))

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(marrySig, types.EmptyATXID, marrySig.NodeID()),
			withMarriageCertificate(signer1, wInitialAtx1.ID(), marrySig.NodeID()),
			withMarriageCertificate(signer2, wInitialAtx2.ID(), marrySig.NodeID()),
		)
		wMarriageAtx.Sign(marrySig)

		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = marrySig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))
		return wMarriageAtx
	}

	t.Run("ATXs must be different", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx := &ActivationTxV2{}
		atx.Sign(signer1)

		proof, err := NewDoubleMergeProof(db, atx, atx)
		require.ErrorContains(t, err, "ATXs have the same ID")
		require.Nil(t, proof)
	})

	t.Run("ATXs must have different signers", func(t *testing.T) {
		// Note: catching this scenario is the responsibility of "invalid previous ATX proof"
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx1 := &ActivationTxV2{}
		atx1.Sign(signer1)

		atx2 := &ActivationTxV2{VRFNonce: 1}
		atx2.Sign(signer1)

		proof, err := NewDoubleMergeProof(db, atx1, atx2)
		require.ErrorContains(t, err, "ATXs have the same smesher")
		require.Nil(t, proof)
	})

	t.Run("ATXs must have marriage ATX", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx := &ActivationTxV2{}
		atx.Sign(signer1)

		atx2 := &ActivationTxV2{VRFNonce: 1}
		atx2.Sign(signer2)

		// ATX 1 has no marriage
		_, err := NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATX 1 have no marriage ATX")

		// ATX 2 has no marriage
		atx.MarriageATX = new(types.ATXID)
		*atx.MarriageATX = types.RandomATXID()
		_, err = NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATX 2 have no marriage ATX")

		// ATX 1 and 2 must have the same marriage ATX
		atx2.MarriageATX = new(types.ATXID)
		*atx2.MarriageATX = types.RandomATXID()
		_, err = NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATXs have different marriage ATXs")
	})

	t.Run("ATXs must be published in the same epoch", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		marriageID := types.RandomATXID()
		atx := &ActivationTxV2{
			MarriageATX: &marriageID,
		}
		atx.Sign(signer1)

		atx2 := &ActivationTxV2{
			MarriageATX:  &marriageID,
			PublishEpoch: 1,
		}
		atx2.Sign(signer2)
		proof, err := NewDoubleMergeProof(db, atx, atx2)
		require.ErrorContains(t, err, "ATXs have different publish epoch")
		require.Nil(t, proof)
	})

	t.Run("valid proof", func(t *testing.T) {
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
		atx1.Sign(signer1)

		atx2 := newActivationTxV2(
			withMarriageATX(marriageAtx.ID()),
			withPublishEpoch(marriageAtx.PublishEpoch+1),
		)
		atx2.Sign(signer2)

		proof, err := NewDoubleMergeProof(db, atx1, atx2)
		require.NoError(t, err)
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, signer1.NodeID(), id)

		require.Equal(t, signer1.NodeID(), proof.SmesherID1)
		require.Equal(t, signer2.NodeID(), proof.SmesherID2)
	})

	t.Run("invalid marriage proof", func(t *testing.T) {
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
		atx1.Sign(signer1)

		atx2 := newActivationTxV2(
			withMarriageATX(marriageAtx.ID()),
			withPublishEpoch(marriageAtx.PublishEpoch+1),
		)
		atx2.Sign(signer2)

		proof, err := NewDoubleMergeProof(db, atx1, atx2)
		require.NoError(t, err)

		hash := proof.MarriageATXProof1[0]
		proof.MarriageATXProof1[0] = types.RandomHash()
		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 1 invalid marriage ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.MarriageATXProof1[0] = hash

		hash = proof.MarriageATXProof2[0]
		proof.MarriageATXProof2[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "ATX 2 invalid marriage ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.MarriageATXProof2[0] = hash
	})
}

func Test_Validate_DoubleMergeProof(t *testing.T) {
	edVerifier := signing.NewEdVerifier()

	ctrl := gomock.NewController(t)
	verifier := NewMockMalfeasanceValidator(ctrl)
	verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
			return edVerifier.Verify(d, nodeID, m, sig)
		}).AnyTimes()

	t.Run("ATXs must have different IDs", func(t *testing.T) {
		t.Parallel()
		id := types.RandomATXID()
		proof := &ProofDoubleMerge{
			ATXID1: id,
			ATXID2: id,
		}
		_, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "ATXs have the same ID")
	})
	t.Run("ATX 1 must have valid signature", func(t *testing.T) {
		t.Parallel()
		proof := &ProofDoubleMerge{
			ATXID1: types.RandomATXID(),
			ATXID2: types.RandomATXID(),
		}
		_, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "ATX 1 invalid signature")
	})
	t.Run("ATX 2 must have valid signature", func(t *testing.T) {
		t.Parallel()

		atx1 := &ActivationTxV2{}
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx1.Sign(signer)

		proof := &ProofDoubleMerge{
			ATXID1:     atx1.ID(),
			SmesherID1: atx1.SmesherID,
			Signature1: atx1.Signature,

			ATXID2: types.RandomATXID(),
		}
		_, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "ATX 2 invalid signature")
	})

	t.Run("epoch proof for ATX1 must be valid", func(t *testing.T) {
		t.Parallel()

		atx1 := &ActivationTxV2{}
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx1.Sign(signer)

		atx2 := &ActivationTxV2{
			NIPosts: []NIPostV2{
				{
					Challenge: types.RandomHash(),
				},
			},
		}
		signer2, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx2.Sign(signer2)

		proof := &ProofDoubleMerge{
			ATXID1:     atx1.ID(),
			SmesherID1: atx1.SmesherID,
			Signature1: atx1.Signature,

			ATXID2:     atx2.ID(),
			SmesherID2: atx2.SmesherID,
			Signature2: atx2.Signature,
		}
		_, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "ATX 1 invalid publish epoch proof")
	})

	t.Run("epoch proof for ATX2 must be valid", func(t *testing.T) {
		t.Parallel()

		atx1 := &ActivationTxV2{}
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx1.Sign(signer)

		atx2 := &ActivationTxV2{
			NIPosts: []NIPostV2{
				{
					Challenge: types.RandomHash(),
				},
			},
		}
		signer2, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx2.Sign(signer2)

		proof := &ProofDoubleMerge{
			ATXID1:             atx1.ID(),
			SmesherID1:         atx1.SmesherID,
			Signature1:         atx1.Signature,
			PublishEpochProof1: atx1.PublishEpochProof(),

			ATXID2:     atx2.ID(),
			SmesherID2: atx2.SmesherID,
			Signature2: atx2.Signature,
		}
		_, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "ATX 2 invalid publish epoch proof")
	})
}
