package wire

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_DoubleMarryProof(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	t.Run("valid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
			withMarriageCertificate(sig, atx1.ID(), otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)
		require.NotNil(t, proof)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, otherSig.NodeID(), id)
	})

	t.Run("does not contain same certificate owner", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		atx1 := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
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
	})

	t.Run("same ATX ID", func(t *testing.T) {
		atx1 := newActivationTxV2()
		atx1.Sign(sig)

		db := statesql.InMemoryTest(t)
		proof, err := NewDoubleMarryProof(db, atx1, atx1, sig.NodeID())
		require.ErrorContains(t, err, "ATXs have the same ID")
		require.Nil(t, proof)

		// manually construct an invalid proof
		proof = &ProofDoubleMarry{
			ATX1: atx1.ID(),
			ATX2: atx1.ID(),
		}

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)

		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "same ATX ID")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid marriage proof", func(t *testing.T) {
		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
			withMarriageCertificate(sig, atx1.ID(), otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		// manually construct an invalid proof
		proof1, err := createMarryProof(db, atx1, otherSig.NodeID())
		require.NoError(t, err)
		proof2, err := createMarryProof(db, atx2, otherSig.NodeID())
		require.NoError(t, err)

		proof := &ProofDoubleMarry{
			NodeID: otherSig.NodeID(),

			ATX1:       atx1.ID(),
			SmesherID1: atx1.SmesherID,
			Signature1: atx1.Signature,
			Proof1:     proof1,

			ATX2:       atx2.ID(),
			SmesherID2: atx2.SmesherID,
			Signature2: atx2.Signature,
			Proof2:     proof2,
		}

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof.Proof1.MarriageCertificatesProof = slices.Clone(proof1.MarriageCertificatesProof)
		proof.Proof1.MarriageCertificatesProof[0] = types.RandomHash()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid marriage proof")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Proof1.MarriageCertificatesProof[0] = proof1.MarriageCertificatesProof[0]
		proof.Proof2.MarriageCertificatesProof = slices.Clone(proof2.MarriageCertificatesProof)
		proof.Proof2.MarriageCertificatesProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid certificate proof", func(t *testing.T) {
		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
			withMarriageCertificate(sig, atx1.ID(), otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		// manually construct an invalid proof
		proof1, err := createMarryProof(db, atx1, otherSig.NodeID())
		require.NoError(t, err)
		proof2, err := createMarryProof(db, atx2, otherSig.NodeID())
		require.NoError(t, err)

		proof := &ProofDoubleMarry{
			NodeID: otherSig.NodeID(),

			ATX1:       atx1.ID(),
			SmesherID1: atx1.SmesherID,
			Signature1: atx1.Signature,
			Proof1:     proof1,

			ATX2:       atx2.ID(),
			SmesherID2: atx2.SmesherID,
			Signature2: atx2.Signature,
			Proof2:     proof2,
		}

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof.Proof1.CertificateProof = slices.Clone(proof1.CertificateProof)
		proof.Proof1.CertificateProof[0] = types.RandomHash()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid certificate proof")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Proof1.CertificateProof[0] = proof1.CertificateProof[0]
		proof.Proof2.CertificateProof = slices.Clone(proof2.CertificateProof)
		proof.Proof2.CertificateProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid certificate proof")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid atx signature", func(t *testing.T) {
		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(sig, atx1.ID(), sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof.Signature1 = types.RandomEdSignature()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX1")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Signature1 = atx1.Signature
		proof.Signature2 = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX2")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("invalid certificate signature", func(t *testing.T) {
		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		atx1 := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(sig, atx1.ID(), sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof.Proof1.Certificate.Signature = types.RandomEdSignature()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid certificate signature")
		require.Equal(t, types.EmptyNodeID, id)

		proof.Proof1.Certificate.Signature = atx1.Marriages[1].Signature
		proof.Proof2.Certificate.Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid certificate signature")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("unknown reference ATX", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		atx1 := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, types.RandomATXID(), sig.NodeID()), // unknown reference ATX
		)
		atx1.Sign(sig)

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(sig, atx1.ID(), sig.NodeID()),
		)
		atx2.Sign(otherSig)

		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.Error(t, err)
		require.Nil(t, proof)
	})
}
