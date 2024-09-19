package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_NewDoubleMergeProof(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	refAtx1 := &types.ActivationTx{}
	refAtx1.SetID(types.RandomATXID())
	refAtx1.SmesherID = signer.NodeID()

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	refAtx2 := &types.ActivationTx{}
	refAtx2.SetID(types.RandomATXID())
	refAtx2.SmesherID = signer2.NodeID()

	marriageID := types.RandomATXID()
	t.Run("ATXs must be different", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx := &ActivationTxV2{}
		atx.Sign(signer)

		proof, err := NewDoubleMergeProof(db, atx, atx, nil)
		require.ErrorContains(t, err, "ATXs have the same ID")
		require.Nil(t, proof)
	})

	t.Run("ATXs must have marriage ATX", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx := &ActivationTxV2{}
		atx.Sign(signer)

		atx2 := &ActivationTxV2{VRFNonce: 1}
		atx2.Sign(signer2)

		// ATX 1 has no marriage
		_, err := NewDoubleMergeProof(db, atx, atx2, nil)
		require.ErrorContains(t, err, "ATX 1 have no marriage ATX")

		// ATX 2 has no marriage
		atx.MarriageATX = &marriageID
		_, err = NewDoubleMergeProof(db, atx, atx2, nil)
		require.ErrorContains(t, err, "ATX 2 have no marriage ATX")

		// ATX 1 and 2 must have the same marriage ATX
		marriageID2 := types.RandomATXID()
		atx2.MarriageATX = &marriageID2
		_, err = NewDoubleMergeProof(db, atx, atx2, nil)
		require.ErrorContains(t, err, "ATXs have different marriage ATXs")
	})
	t.Run("ATXs must be published in the same epoch", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx := &ActivationTxV2{
			MarriageATX: &marriageID,
		}
		atx.Sign(signer)

		atx2 := &ActivationTxV2{
			MarriageATX:  &marriageID,
			PublishEpoch: 1,
		}
		atx2.Sign(signer2)
		proof, err := NewDoubleMergeProof(db, atx, atx2, nil)
		require.ErrorContains(t, err, "ATXs have different publish epoch")
		require.Nil(t, proof)
	})
	t.Run("valid proof", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		signer3, err := signing.NewEdSigner()
		require.NoError(t, err)

		marriageAtx := &ActivationTxV2{
			Marriages: []MarriageCertificate{
				{
					ReferenceAtx: refAtx1.ID(),
					// signer marries signer3
					Signature: signer.Sign(signing.MARRIAGE, signer3.NodeID().Bytes()),
				},
				{
					ReferenceAtx: refAtx2.ID(),
					// signer2 marries signer3
					Signature: signer2.Sign(signing.MARRIAGE, signer3.NodeID().Bytes()),
				},
			},
		}
		marriageAtx.Sign(signer3)
		marriageID := marriageAtx.ID()

		require.NoError(t, atxs.Add(db, refAtx1, types.AtxBlob{}))
		require.NoError(t, atxs.Add(db, refAtx2, types.AtxBlob{}))
		atx := &ActivationTxV2{
			MarriageATX:  &marriageID,
			PublishEpoch: 1,
		}
		atx.Sign(signer)

		atx2 := &ActivationTxV2{
			MarriageATX:  &marriageID,
			PublishEpoch: 1,
			VRFNonce:     1,
		}
		atx2.Sign(signer2)

		proof, err := NewDoubleMergeProof(db, atx, atx2, marriageAtx)
		require.NoError(t, err)
		require.NotNil(t, proof)
		require.Equal(t, atx.PublishEpoch, proof.PublishEpoch)
		require.Equal(t, marriageID, proof.MarriageProof.ATXID)
		require.Equal(t, marriageAtx.SmesherID, proof.MarriageProof.SmesherID)
		require.Equal(t, marriageAtx.Signature, proof.MarriageProof.Signature)

		id, err := proof.Valid(signing.NewEdVerifier())
		require.NoError(t, err)
		require.Equal(t, atx.SmesherID, id)

		atxs := []*ActivationTxV2{atx, atx2}
		for i, p := range proof.Proofs {
			require.Equal(t, atxs[i].ID(), p.ATXID)
			require.Equal(t, atxs[i].SmesherID, p.SmesherID)
			require.Equal(t, atxs[i].Signature, p.Signature)
		}
	})
}

func Test_Validate_DoubleMergeProof(t *testing.T) {
	t.Run("ATXs must have different IDs", func(t *testing.T) {
		t.Parallel()
		id := types.RandomATXID()
		proof := &ProofDoubleMerge{
			Proofs: [2]MergeProof{
				{ATXID: id},
				{ATXID: id},
			},
		}
		_, err := proof.Valid(signing.NewEdVerifier())
		require.ErrorContains(t, err, "ATXs have the same ID")
	})
	t.Run("ATX 1 must have valid signature", func(t *testing.T) {
		t.Parallel()
		proof := &ProofDoubleMerge{
			Proofs: [2]MergeProof{
				{ATXID: types.RandomATXID()},
				{ATXID: types.RandomATXID()},
			},
		}
		_, err := proof.Valid(signing.NewEdVerifier())
		require.ErrorContains(t, err, "ATX 1 invalid signature")
	})
	t.Run("ATX 2 must have valid signature", func(t *testing.T) {
		t.Parallel()

		atx1 := &ActivationTxV2{}
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx1.Sign(signer)
		proof := &ProofDoubleMerge{
			Proofs: [2]MergeProof{
				{ATXID: atx1.ID(), SmesherID: atx1.SmesherID, Signature: atx1.Signature},
				{ATXID: types.RandomATXID()},
			},
		}
		_, err = proof.Valid(signing.NewEdVerifier())
		require.ErrorContains(t, err, "ATX 2 invalid signature")
	})
}

func Test_MergeProof_Validation(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	marriageATXID := types.RandomATXID()
	atx := &ActivationTxV2{
		MarriageATX:  &marriageATXID,
		PublishEpoch: 12,
	}
	atx.Sign(signer)

	proof, err := newMergeProof(atx)
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		err := proof.valid(signing.NewEdVerifier(), atx.PublishEpoch, marriageATXID)
		require.NoError(t, err)
	})

	t.Run("invalid epoch", func(t *testing.T) {
		t.Parallel()
		err := proof.valid(signing.NewEdVerifier(), atx.PublishEpoch+1, marriageATXID)
		require.Error(t, err)
	})

	t.Run("invalid marriage ATX", func(t *testing.T) {
		t.Parallel()
		err := proof.valid(signing.NewEdVerifier(), atx.PublishEpoch, types.RandomATXID())
		require.Error(t, err)
	})

	t.Run("invalid signature", func(t *testing.T) {
		t.Parallel()
		invalidProof := *proof
		invalidProof.Signature = types.RandomEdSignature()
		err := invalidProof.valid(signing.NewEdVerifier(), atx.PublishEpoch, marriageATXID)
		require.Error(t, err)
	})
}
