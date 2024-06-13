package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func Test_DoubleMarryProof(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, sig.NodeID()),
			WithMarriageCertificate(otherSig, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, otherSig.NodeID()),
			WithMarriageCertificate(sig, otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)

		verifier := signing.NewEdVerifier()
		ok, err := proof.Valid(verifier)
		require.NoError(t, err)
		require.True(t, ok)
	})

	t.Run("does not contain same certificate owner", func(t *testing.T) {
		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, otherSig.NodeID()),
		)
		atx2.Sign(sig)

		proof, err := NewDoubleMarryProof(atx1, atx2, otherSig.NodeID())
		require.ErrorContains(t, err, "ATX 1 does not contain a marriage certificate signed by the given node ID")
		require.Nil(t, proof)

		proof, err = NewDoubleMarryProof(atx1, atx2, sig.NodeID())
		require.ErrorContains(t, err, "ATX 2 does not contain a marriage certificate signed by the given node ID")
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
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proofs have different node IDs")
		require.False(t, ok)
	})

	t.Run("same ATX ID", func(t *testing.T) {
		atx1 := newActivationTxV2()
		atx1.Sign(sig)

		proof, err := NewDoubleMarryProof(atx1, atx1, sig.NodeID())
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
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "same ATX ID")
		require.False(t, ok)
	})

	t.Run("invalid marriage proof", func(t *testing.T) {
		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, sig.NodeID()),
			WithMarriageCertificate(otherSig, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, sig.NodeID()),
			WithMarriageCertificate(sig, sig.NodeID()),
		)
		atx2.Sign(otherSig)

		// manually construct an invalid proof
		proof1, err := marriageProof(atx1)
		require.NoError(t, err)
		proof2, err := marriageProof(atx2)
		require.NoError(t, err)
		certProof1, err := certificateProof(atx1.Marriages, 0)
		require.NoError(t, err)
		certProof2, err := certificateProof(atx2.Marriages, 1)
		require.NoError(t, err)

		proof := &ProofDoubleMarry{
			Proofs: [2]MarryProof{
				{
					ATXID:                atx1.ID(),
					NodeID:               sig.NodeID(),
					MarriageRoot:         types.Hash32(atx1.Marriages.Root()),
					MarriageProof:        proof1,
					CertificateSignature: atx1.Marriages[0].Signature,
					CertificateIndex:     0,
					CertificateProof:     certProof1,
					SmesherID:            atx1.SmesherID,
					Signature:            atx1.Signature,
				},
				{
					ATXID:                atx2.ID(),
					NodeID:               sig.NodeID(),
					MarriageRoot:         types.Hash32(atx2.Marriages.Root()),
					MarriageProof:        proof2,
					CertificateSignature: atx2.Marriages[1].Signature,
					CertificateIndex:     1,
					CertificateProof:     certProof2,
					SmesherID:            atx2.SmesherID,
					Signature:            atx2.Signature,
				},
			},
		}

		verifier := signing.NewEdVerifier()
		proof.Proofs[0].MarriageProof[0] = types.RandomHash()
		ok, err := proof.Valid(verifier)
		require.NoError(t, err)
		require.False(t, ok)

		proof.Proofs[0].MarriageProof[0] = proof1[0]
		proof.Proofs[1].MarriageProof[0] = types.RandomHash()
		ok, err = proof.Valid(verifier)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("invalid certificate proof", func(t *testing.T) {
		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, sig.NodeID()),
			WithMarriageCertificate(otherSig, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, sig.NodeID()),
			WithMarriageCertificate(sig, sig.NodeID()),
		)
		atx2.Sign(otherSig)

		// manually construct an invalid proof
		proof1, err := marriageProof(atx1)
		require.NoError(t, err)
		proof2, err := marriageProof(atx2)
		require.NoError(t, err)
		certProof1, err := certificateProof(atx1.Marriages, 0)
		require.NoError(t, err)
		certProof2, err := certificateProof(atx2.Marriages, 1)
		require.NoError(t, err)

		proof := &ProofDoubleMarry{
			Proofs: [2]MarryProof{
				{
					ATXID:                atx1.ID(),
					NodeID:               sig.NodeID(),
					MarriageRoot:         types.Hash32(atx1.Marriages.Root()),
					MarriageProof:        proof1,
					CertificateSignature: atx1.Marriages[0].Signature,
					CertificateIndex:     0,
					CertificateProof:     certProof1,
					SmesherID:            atx1.SmesherID,
					Signature:            atx1.Signature,
				},
				{
					ATXID:                atx2.ID(),
					NodeID:               sig.NodeID(),
					MarriageRoot:         types.Hash32(atx2.Marriages.Root()),
					MarriageProof:        proof2,
					CertificateSignature: atx2.Marriages[1].Signature,
					CertificateIndex:     1,
					CertificateProof:     certProof2,
					SmesherID:            atx2.SmesherID,
					Signature:            atx2.Signature,
				},
			},
		}

		verifier := signing.NewEdVerifier()
		proof.Proofs[0].CertificateProof[0] = types.RandomHash()
		ok, err := proof.Valid(verifier)
		require.NoError(t, err)
		require.False(t, ok)

		proof.Proofs[0].CertificateProof[0] = certProof1[0]
		proof.Proofs[1].CertificateProof[0] = types.RandomHash()
		ok, err = proof.Valid(verifier)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("invalid atx signature", func(t *testing.T) {
		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, sig.NodeID()),
			WithMarriageCertificate(otherSig, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, sig.NodeID()),
			WithMarriageCertificate(sig, sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)

		verifier := signing.NewEdVerifier()

		proof.Proofs[0].Signature = types.RandomEdSignature()
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.False(t, ok)

		proof.Proofs[0].Signature = atx1.Signature
		proof.Proofs[1].Signature = types.RandomEdSignature()
		ok, err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.False(t, ok)
	})

	t.Run("invalid certificate signature", func(t *testing.T) {
		atx1 := newActivationTxV2(
			WithMarriageCertificate(sig, sig.NodeID()),
			WithMarriageCertificate(otherSig, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			WithMarriageCertificate(otherSig, sig.NodeID()),
			WithMarriageCertificate(sig, sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)

		verifier := signing.NewEdVerifier()

		proof.Proofs[0].CertificateSignature = types.RandomEdSignature()
		ok, err := proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid certificate signature")
		require.False(t, ok)

		proof.Proofs[0].CertificateSignature = atx1.Marriages[1].Signature
		proof.Proofs[1].CertificateSignature = types.RandomEdSignature()
		ok, err = proof.Valid(verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid certificate signature")
		require.False(t, ok)
	})
}
