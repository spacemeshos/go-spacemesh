package wire

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

func Test_DoubleMarryProof(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		db := sql.InMemory()
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
			WithMarriageCertificate(sig, atx1.ID(), otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)
		require.NotNil(t, proof)

		verifier := signing.NewEdVerifier()
		id, err := proof.Valid(verifier)
		require.NoError(t, err)
		require.Equal(t, otherSig.NodeID(), id)
	})

	t.Run("does not contain same certificate owner", func(t *testing.T) {
		db := sql.InMemory()

		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.ErrorContains(t, err, fmt.Sprintf(
			"proof for atx1: does not contain a marriage certificate signed by %s", otherSig.NodeID().ShortString(),
		))
		require.Nil(t, proof)

		proof, err = NewDoubleMarryProof(db, atx1, atx2, sig.NodeID())
		require.ErrorContains(t, err, fmt.Sprintf(
			"proof for atx2: does not contain a marriage certificate signed by %s", sig.NodeID().ShortString(),
		))
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof = &ProofDoubleMarry{
			Proofs: [2]MarryProof{
				{
					ATXID:  atx1.ID(),
					NodeID: sig.NodeID(),
				},
				{
					ATXID:  atx2.ID(),
					NodeID: otherSig.NodeID(),
				},
			},
		}

		verifier := signing.NewEdVerifier()
		id, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proofs have different node IDs")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("same ATX ID", func(t *testing.T) {
		atx1 := newActivationTxV2()
		atx1.Sign(sig)

		db := sql.InMemory()
		proof, err := NewDoubleMarryProof(db, atx1, atx1, sig.NodeID())
		require.ErrorContains(t, err, "ATXs have the same ID")
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof = &ProofDoubleMarry{
			Proofs: [2]MarryProof{
				{
					ATXID: atx1.ID(),
				},
				{
					ATXID: atx1.ID(),
				},
			},
		}

		verifier := signing.NewEdVerifier()
		id, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "same ATX ID")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid marriage proof", func(t *testing.T) {
		db := sql.InMemory()
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
			WithMarriageCertificate(sig, atx1.ID(), otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		// manually construct an invalid proof
		proof1, err := createMarryProof(db, atx1, otherSig.NodeID())
		require.NoError(t, err)
		proof2, err := createMarryProof(db, atx2, otherSig.NodeID())
		require.NoError(t, err)

		proof := &ProofDoubleMarry{
			Proofs: [2]MarryProof{
				proof1, proof2,
			},
		}

		verifier := signing.NewEdVerifier()
		proof.Proofs[0].MarriageProof = slices.Clone(proof1.MarriageProof)
		proof.Proofs[0].MarriageProof[0] = types.RandomHash()
		id, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid marriage proof")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Proofs[0].MarriageProof[0] = proof1.MarriageProof[0]
		proof.Proofs[1].MarriageProof = slices.Clone(proof2.MarriageProof)
		proof.Proofs[1].MarriageProof[0] = types.RandomHash()
		id, err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid certificate proof", func(t *testing.T) {
		db := sql.InMemory()
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
			WithMarriageCertificate(sig, atx1.ID(), otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		// manually construct an invalid proof
		proof1, err := createMarryProof(db, atx1, otherSig.NodeID())
		require.NoError(t, err)
		proof2, err := createMarryProof(db, atx2, otherSig.NodeID())
		require.NoError(t, err)

		proof := &ProofDoubleMarry{
			Proofs: [2]MarryProof{
				proof1, proof2,
			},
		}

		verifier := signing.NewEdVerifier()
		proof.Proofs[0].CertificateProof = slices.Clone(proof1.CertificateProof)
		proof.Proofs[0].CertificateProof[0] = types.RandomHash()
		id, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid certificate proof")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Proofs[0].CertificateProof[0] = proof1.CertificateProof[0]
		proof.Proofs[1].CertificateProof = slices.Clone(proof2.CertificateProof)
		proof.Proofs[1].CertificateProof[0] = types.RandomHash()
		id, err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid certificate proof")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid atx signature", func(t *testing.T) {
		db := sql.InMemory()
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(sig, atx1.ID(), sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)

		verifier := signing.NewEdVerifier()

		proof.Proofs[0].Signature = types.RandomEdSignature()
		id, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Proofs[0].Signature = atx1.Signature
		proof.Proofs[1].Signature = types.RandomEdSignature()
		id, err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid certificate signature", func(t *testing.T) {
		db := sql.InMemory()
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(sig, atx1.ID(), sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)

		verifier := signing.NewEdVerifier()

		proof.Proofs[0].CertificateSignature = types.RandomEdSignature()
		id, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid certificate signature")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Proofs[0].CertificateSignature = atx1.Marriages[1].Signature
		proof.Proofs[1].CertificateSignature = types.RandomEdSignature()
		id, err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid certificate signature")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("unknown reference ATX", func(t *testing.T) {
		db := sql.InMemory()

		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(otherSig, types.RandomATXID(), sig.NodeID()), // unknown reference ATX
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, types.EmptyATXID, sig.NodeID()),
			WithMarriageCertificate(sig, atx1.ID(), sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.Error(t, err)
		require.Nil(t, proof)
	})
}
