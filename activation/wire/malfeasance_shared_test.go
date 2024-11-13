package wire

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_MarryProof(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

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

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// valid for otherSig
		proof, err := createMarryProof(db, atx1, otherSig.NodeID())
		require.NoError(t, err)
		require.NotEmpty(t, proof)

		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), otherSig.NodeID())
		require.NoError(t, err)

		// valid for sig
		proof, err = createMarryProof(db, atx1, sig.NodeID())
		require.NoError(t, err)
		require.NotEmpty(t, proof)

		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), sig.NodeID())
		require.NoError(t, err)
	})

	t.Run("identity not included in certificates", func(t *testing.T) {
		t.Parallel()

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

		nodeID := types.RandomNodeID()
		proof, err := createMarryProof(db, atx1, nodeID)
		require.EqualError(t, err,
			fmt.Sprintf("does not contain a marriage certificate signed by %s", nodeID.ShortString()),
		)
		require.Empty(t, proof)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()

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

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof, err := createMarryProof(db, atx1, otherSig.NodeID())
		require.NoError(t, err)
		require.NotEmpty(t, proof)

		// not valid for random NodeID
		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), types.RandomNodeID())
		require.EqualError(t, err, "invalid certificate signature")

		// not valid for another ATX
		err = proof.Valid(verifier, types.RandomATXID(), sig.NodeID(), otherSig.NodeID())
		require.EqualError(t, err, "invalid marriage proof")

		// not valid if certificate signature is invalid
		certSig := proof.Certificate.Signature
		proof.Certificate.Signature = types.RandomEdSignature()
		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), otherSig.NodeID())
		require.EqualError(t, err, "invalid certificate signature")
		proof.Certificate.Signature = certSig

		// not valid if marriage root is invalid
		marriageRoot := proof.MarriageCertificatesRoot
		proof.MarriageCertificatesRoot = MarriageCertificatesRoot(types.RandomHash())
		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), otherSig.NodeID())
		require.EqualError(t, err, "invalid marriage proof")
		proof.MarriageCertificatesRoot = marriageRoot

		// not valid if marriage root proof is invalid
		hash := proof.MarriageCertificatesProof[0]
		proof.MarriageCertificatesProof[0] = types.RandomHash()
		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), otherSig.NodeID())
		require.EqualError(t, err, "invalid marriage proof")
		proof.MarriageCertificatesProof[0] = hash

		// not valid if certificate proof is invalid
		index := proof.CertificateIndex
		proof.CertificateIndex = 100
		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), otherSig.NodeID())
		require.EqualError(t, err, "invalid certificate proof")
		proof.CertificateIndex = index

		certProof := proof.CertificateProof
		proof.CertificateProof = MarriageCertificateProof{types.RandomHash()}
		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), otherSig.NodeID())
		require.EqualError(t, err, "invalid certificate proof")
		proof.CertificateProof = certProof

		hash = proof.CertificateProof[0]
		proof.CertificateProof[0] = types.RandomHash()
		err = proof.Valid(verifier, atx1.ID(), sig.NodeID(), otherSig.NodeID())
		require.EqualError(t, err, "invalid certificate proof")
		proof.CertificateProof[0] = hash
	})
}

func Test_MarriageProof(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		wMarriageAtx.Sign(sig)
		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withMarriageATX(wMarriageAtx.ID()),
		)
		atx.Sign(sig)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof, err := createMarriageProof(db, atx, otherSig.NodeID())
		require.NoError(t, err)
		require.NotEmpty(t, proof)

		err = proof.Valid(verifier, atx.ID(), otherSig.NodeID(), sig.NodeID())
		require.NoError(t, err)
	})

	t.Run("node ID is the same as smesher ID", func(t *testing.T) {
		t.Parallel()

		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		wMarriageAtx.Sign(sig)
		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withMarriageATX(wMarriageAtx.ID()),
		)
		atx.Sign(sig)

		proof, err := createMarriageProof(db, atx, sig.NodeID())
		require.EqualError(t, err, "node ID is the same as smesher ID")
		require.Empty(t, proof)
	})

	t.Run("marriage ATX is not available", func(t *testing.T) {
		t.Parallel()

		db := statesql.InMemoryTest(t)

		atx := newActivationTxV2(
			withMarriageATX(types.RandomATXID()),
		)
		atx.Sign(sig)

		proof, err := createMarriageProof(db, atx, otherSig.NodeID())
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Empty(t, proof)
	})

	t.Run("node ID isn't married in marriage ATX", func(t *testing.T) {
		t.Parallel()

		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		wMarriageAtx.Sign(sig)
		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withMarriageATX(wMarriageAtx.ID()),
		)
		atx.Sign(sig)

		invalidSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		proof, err := createMarriageProof(db, atx, invalidSig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("does not contain a marriage certificate signed by %s", invalidSig.NodeID().ShortString()),
		)
		require.Empty(t, proof)

		atx.Sign(invalidSig)
		proof, err = createMarriageProof(db, atx, otherSig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("does not contain a marriage certificate signed by %s", invalidSig.NodeID().ShortString()),
		)
		require.Empty(t, proof)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()

		db := statesql.InMemoryTest(t)
		otherAtx := &types.ActivationTx{}
		otherAtx.SetID(types.RandomATXID())
		otherAtx.SmesherID = otherSig.NodeID()
		require.NoError(t, atxs.Add(db, otherAtx, types.AtxBlob{}))

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(sig, types.EmptyATXID, sig.NodeID()),
			withMarriageCertificate(otherSig, otherAtx.ID(), sig.NodeID()),
		)
		wMarriageAtx.Sign(sig)
		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withMarriageATX(wMarriageAtx.ID()),
		)
		atx.Sign(sig)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof, err := createMarriageProof(db, atx, otherSig.NodeID())
		require.NoError(t, err)
		require.NotEmpty(t, proof)

		// not valid for random ATX
		err = proof.Valid(verifier, types.RandomATXID(), otherSig.NodeID(), sig.NodeID())
		require.EqualError(t, err, "invalid marriage ATX proof")

		// not valid for another smesher
		err = proof.Valid(verifier, atx.ID(), otherSig.NodeID(), types.RandomNodeID())
		require.ErrorContains(t, err, "invalid certificate signature")

		// not valid for another nodeID
		err = proof.Valid(verifier, atx.ID(), types.RandomNodeID(), sig.NodeID())
		require.ErrorContains(t, err, "invalid certificate signature")

		// not valid for incorrect marriage ATX
		marriageATX := proof.MarriageATX
		proof.MarriageATX = types.RandomATXID()
		err = proof.Valid(verifier, atx.ID(), otherSig.NodeID(), sig.NodeID())
		require.EqualError(t, err, "invalid marriage ATX proof")
		proof.MarriageATX = marriageATX

		// not valid for incorrect marriage ATX smesher ID
		marriageATXSmesherID := proof.MarriageATXSmesherID
		proof.MarriageATXSmesherID = types.RandomNodeID()
		err = proof.Valid(verifier, atx.ID(), otherSig.NodeID(), sig.NodeID())
		require.ErrorContains(t, err, "invalid certificate signature")
		proof.MarriageATXSmesherID = marriageATXSmesherID
	})
}
