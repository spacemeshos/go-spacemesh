package wire

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_DoubleMarryProof(t *testing.T) {
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

	t.Run("identity is not included in both ATXs", func(t *testing.T) {
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

		atx2 := newActivationTxV2(
			withMarriageCertificate(otherSig, types.EmptyATXID, otherSig.NodeID()),
			withMarriageCertificate(sig, atx1.ID(), otherSig.NodeID()),
		)
		atx2.Sign(otherSig)

		marriages := make([]MarriageCertificate, len(atx1.Marriages))
		copy(marriages, atx1.Marriages)
		atx1.Marriages = marriages[:1]
		proof, err := NewDoubleMarryProof(db, atx1, atx2, otherSig.NodeID())
		require.EqualError(t, err, fmt.Sprintf(
			"proof for atx1: does not contain a marriage certificate signed by %s", otherSig.NodeID().ShortString(),
		))
		require.Nil(t, proof)
		atx1.Marriages = marriages

		marriages = make([]MarriageCertificate, len(atx2.Marriages))
		copy(marriages, atx2.Marriages)
		atx2.Marriages = marriages[:1]
		proof, err = NewDoubleMarryProof(db, atx1, atx2, sig.NodeID())
		require.EqualError(t, err, fmt.Sprintf(
			"proof for atx2: does not contain a marriage certificate signed by %s", sig.NodeID().ShortString(),
		))
		require.Nil(t, proof)
		atx2.Marriages = marriages
	})

	t.Run("same ATX ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		atx1 := newActivationTxV2()
		atx1.Sign(sig)

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

		// invalid signature for ATX1
		proof.Signature1 = types.RandomEdSignature()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX1")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Signature1 = atx1.Signature

		// invalid signature for ATX2
		proof.Signature2 = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX2")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Signature2 = atx2.Signature

		// invalid smesher ID for ATX1
		proof.SmesherID1 = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX1")
		require.Equal(t, types.EmptyNodeID, id)
		proof.SmesherID1 = atx1.SmesherID

		// invalid smesher ID for ATX2
		proof.SmesherID2 = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX2")
		require.Equal(t, types.EmptyNodeID, id)
		proof.SmesherID2 = atx2.SmesherID

		// invalid ATX ID for ATX1
		proof.ATX1 = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX1")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATX1 = atx1.ID()

		// invalid ATX ID for ATX2
		proof.ATX2 = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid signature for ATX2")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATX2 = atx2.ID()
	})
}
