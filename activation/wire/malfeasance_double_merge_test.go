package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func createValidProofDoubleMerge(
	t *testing.T,
	db sql.Executor,
	signer1, signer2, marriageSigner *signing.EdSigner,
) *ProofDoubleMerge {
	refAtx1 := &types.ActivationTx{}
	refAtx1.SetID(types.RandomATXID())
	refAtx1.SmesherID = signer1.NodeID()

	refAtx2 := &types.ActivationTx{}
	refAtx2.SetID(types.RandomATXID())
	refAtx2.SmesherID = signer2.NodeID()

	marriageAtx := &ActivationTxV2{
		Marriages: []MarriageCertificate{
			{
				ReferenceAtx: refAtx1.ID(),
				// signer marries signer3
				Signature: signer1.Sign(signing.MARRIAGE, marriageSigner.NodeID().Bytes()),
			},
			{
				ReferenceAtx: refAtx2.ID(),
				// signer2 marries signer3
				Signature: signer2.Sign(signing.MARRIAGE, marriageSigner.NodeID().Bytes()),
			},
		},
	}
	marriageAtx.Sign(marriageSigner)
	marriageID := marriageAtx.ID()

	require.NoError(t, atxs.Add(db, refAtx1, types.AtxBlob{}))
	require.NoError(t, atxs.Add(db, refAtx2, types.AtxBlob{}))
	atx := &ActivationTxV2{
		MarriageATX:  &marriageID,
		PublishEpoch: 1,
	}
	atx.Sign(signer1)

	atx2 := &ActivationTxV2{
		MarriageATX:  &marriageID,
		PublishEpoch: 1,
		VRFNonce:     1, // for a different ID
	}
	atx2.Sign(signer2)

	proof, err := NewDoubleMergeProof(db, atx, atx2, marriageAtx)
	require.NoError(t, err)
	require.NotNil(t, proof)

	return proof
}

func Test_NewDoubleMergeProof(t *testing.T) {
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

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
	t.Run("ATXs must have different signers", func(t *testing.T) {
		// Note: catching this scenario is the responsibility of "invalid previous ATX proof"
		t.Parallel()
		db := statesql.InMemoryTest(t)
		atx1 := &ActivationTxV2{}
		atx1.Sign(signer)

		atx2 := &ActivationTxV2{VRFNonce: 1}
		atx2.Sign(signer)

		proof, err := NewDoubleMergeProof(db, atx1, atx2, nil)
		require.ErrorContains(t, err, "ATXs have the same smesher")
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

		proof := createValidProofDoubleMerge(t, db, signer, signer2, signer3)
		require.Equal(t, signer3.NodeID(), proof.MarriageProof.SmesherID)

		id, err := proof.Valid(signing.NewEdVerifier())
		require.NoError(t, err)
		require.Equal(t, signer.NodeID(), id)

		signers := []*signing.EdSigner{signer, signer2}
		for i, p := range proof.Proofs {
			require.Equal(t, signers[i].NodeID(), p.SmesherID)
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

func Test_MarriageProof_Validation(t *testing.T) {
	db := statesql.InMemoryTest(t)
	edVerifier := signing.NewEdVerifier()

	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)

	refAtx1 := &types.ActivationTx{}
	refAtx1.SetID(types.RandomATXID())
	refAtx1.SmesherID = signer1.NodeID()
	require.NoError(t, atxs.Add(db, refAtx1, types.AtxBlob{}))

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	marriageAtx := &ActivationTxV2{
		Marriages: []MarriageCertificate{
			{
				ReferenceAtx: types.EmptyATXID,
				Signature:    signer2.Sign(signing.MARRIAGE, signer2.NodeID().Bytes()),
			},
			{
				ReferenceAtx: refAtx1.ID(),
				Signature:    signer1.Sign(signing.MARRIAGE, signer2.NodeID().Bytes()),
			},
		},
	}
	marriageAtx.Sign(signer2)

	t.Run("sanity - no crash on empty", func(t *testing.T) {
		t.Parallel()
		proof := MarriageProof{}
		require.Error(t, proof.valid(edVerifier, types.NodeID{}, types.NodeID{}))
	})
	t.Run("sanity - created is valid", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		require.NoError(t, proof.valid(edVerifier, signer1.NodeID(), signer2.NodeID()))
	})
	t.Run("ID1 is not married", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		require.Error(t, proof.valid(edVerifier, types.RandomNodeID(), signer2.NodeID()))
	})
	t.Run("ID2 is not married", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		require.Error(t, proof.valid(edVerifier, signer1.NodeID(), types.RandomNodeID()))
	})
	t.Run("IDs married but wrong order", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		require.Error(t, proof.valid(edVerifier, signer2.NodeID(), signer1.NodeID()))
	})
	t.Run("invalid signature", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		proof.Signature = types.RandomEdSignature()
		require.Error(t, proof.valid(edVerifier, signer1.NodeID(), signer2.NodeID()))
	})
	t.Run("invalid marriage root hash", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		proof.MarriageRoot[0] ^= 0xFF
		require.Error(t, proof.valid(edVerifier, signer1.NodeID(), signer2.NodeID()))
	})
	t.Run("invalid marriage proof", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		proof.MarriageProof[0][0] ^= 0xFF
		require.Error(t, proof.valid(edVerifier, signer1.NodeID(), signer2.NodeID()))
	})
	t.Run("invalid certificate proof", func(t *testing.T) {
		t.Parallel()
		proof, err := newMarriageProof(db, marriageAtx, signer1.NodeID(), signer2.NodeID())
		require.NoError(t, err)
		proof.CertificateProof[0][0] ^= 0xFF
		require.Error(t, proof.valid(edVerifier, signer1.NodeID(), signer2.NodeID()))
	})
}
