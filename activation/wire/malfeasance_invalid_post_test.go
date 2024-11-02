package wire

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_InvalidPostProof(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// pubSig is the identity that publishes the ATX with the invalid PoST
	pubSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// marrySig is the identity that publishes the marriage ATX
	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	t.Run("valid", func(t *testing.T) {
		// TODO(mafa): implement
	})

	t.Run("valid merged atx", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		wInitialAtx := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wInitialAtx.Sign(sig)
		initialAtx := &types.ActivationTx{
			CommitmentATX: &wInitialAtx.Initial.CommitmentATX,
		}
		initialAtx.SetID(wInitialAtx.ID())
		initialAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, initialAtx, wInitialAtx.Blob()))

		wPubInitialAtx := newActivationTxV2(
			withInitial(types.RandomATXID(), PostV1{}),
		)
		wPubInitialAtx.Sign(pubSig)
		pubInitialAtx := &types.ActivationTx{}
		pubInitialAtx.SetID(wPubInitialAtx.ID())
		pubInitialAtx.SmesherID = pubSig.NodeID()
		require.NoError(t, atxs.Add(db, pubInitialAtx, wPubInitialAtx.Blob()))

		marryInitialAtx := types.RandomATXID()

		wMarriageAtx := newActivationTxV2(
			withMarriageCertificate(marrySig, types.EmptyATXID, marrySig.NodeID()),
			withMarriageCertificate(sig, wInitialAtx.ID(), marrySig.NodeID()),
			withMarriageCertificate(pubSig, wPubInitialAtx.ID(), marrySig.NodeID()),
		)
		wMarriageAtx.Sign(marrySig)

		marriageAtx := &types.ActivationTx{}
		marriageAtx.SetID(wMarriageAtx.ID())
		marriageAtx.SmesherID = pubSig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		nipostChallenge := types.RandomHash()
		invalidPost := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		const numUnits = uint32(11)
		const invalidPostIndex = 7
		atx := newActivationTxV2(
			withPreviousATXs(marryInitialAtx, wInitialAtx.ID(), wPubInitialAtx.ID()),
			withMarriageATX(wMarriageAtx.ID()),
			withNIPost(
				withNIPostChallenge(nipostChallenge),
				withNIPostMembershipProof(MerkleProofV2{}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 0,
					PrevATXIndex:  0,
					Post:          PostV1{},
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 1,
					PrevATXIndex:  1,
					Post:          invalidPost,
					NumUnits:      numUnits,
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 2,
					PrevATXIndex:  2,
					Post:          PostV1{},
				}),
			),
		)
		atx.Sign(pubSig)

		proof, err := NewInvalidPostProof(db, atx, wInitialAtx, sig.NodeID(), 0, invalidPostIndex)
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		verifier.EXPECT().PostIndex(
			context.Background(),
			sig.NodeID(),
			*initialAtx.CommitmentATX,
			PostFromWireV1(&invalidPost),
			nipostChallenge.Bytes(),
			numUnits,
			invalidPostIndex,
		).Return(errors.New("invalid post"))

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
