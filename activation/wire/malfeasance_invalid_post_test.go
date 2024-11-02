package wire

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_InvalidPostProof_MergedATX(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// pubSig is the identity that publishes the ATX with the invalid PoST
	pubSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// marrySig is the identity that publishes the marriage ATX
	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)
		commitmentATX := types.RandomATXID()

		wInitialAtx := newActivationTxV2(
			withInitial(commitmentATX, PostV1{}),
		)
		wInitialAtx.Sign(sig)
		initialAtx := &types.ActivationTx{}
		initialAtx.SetID(wInitialAtx.ID())
		initialAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, initialAtx, wInitialAtx.Blob()))

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(pubSig, types.EmptyATXID, pubSig.NodeID()),
			withMarriageCertificate(sig, wInitialAtx.ID(), pubSig.NodeID()),
		)
		wMarriageAtx.Sign(marrySig)

		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = pubSig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withMarriageATX(wMarriageAtx.ID()),
			// TODO(mafa): add post referencing marriage certificate `1`
		)
		atx.Sign(pubSig)

		proof, err := NewInvalidPostProof(db, atx, wInitialAtx, sig.NodeID(), 0, 3)
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)

		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("post is valid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("commitment is invalid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("marriage ATX is invalid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for commitment", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for invalid post", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for marriage ATX", func(t *testing.T) {
		// TODO(mafa): implement
	})
}

func Test_InvalidPostProof_SoloATX(t *testing.T) {
	_, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("post is valid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("commitment is invalid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("marriage ATX is invalid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for commitment", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for invalid post", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("invalid signature for marriage ATX", func(t *testing.T) {
		// TODO(mafa): implement
	})
}
