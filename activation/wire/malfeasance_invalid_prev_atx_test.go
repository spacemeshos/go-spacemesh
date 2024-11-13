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
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_InvalidPrevAtxProofV2(t *testing.T) {
	t.Parallel()

	// sig is the identity that creates the ATXs referencing the same prevATX
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// pubSig is the identity that publishes a merged ATX with the same prevATX
	pubSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// marrySig is the identity that publishes the marriage ATX
	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	newMergedATXv2 := func(
		db sql.Executor,
		prevATX types.ATXID,
	) *ActivationTxV2 {
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
		marriageAtx.SmesherID = marrySig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withPreviousATXs(marryInitialAtx, wPubInitialAtx.ID(), prevATX),
			withMarriageATX(wMarriageAtx.ID()),
			withNIPost(
				withNIPostMembershipProof(MerkleProofV2{}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 0,
					PrevATXIndex:  0,
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 1,
					PrevATXIndex:  2,
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 2,
					PrevATXIndex:  1,
				}),
			),
		)
		atx.Sign(pubSig)
		return atx
	}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(7),
		)
		atx2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// verify the proof
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("valid merged & solo atx", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		prevAtx := &types.ActivationTx{}
		prevAtx.SetID(prevATXID)
		prevAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, prevAtx, types.AtxBlob{}))
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newMergedATXv2(db, prevATXID)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// verify the proof
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	// valid merged & merged is covered by either double marry or double merge proofs

	t.Run("same ATX ID", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		atx1 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
		)
		atx1.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx1, sig.NodeID())
		require.ErrorContains(t, err, "ATXs have the same ID")
		require.Nil(t, proof)
	})

	t.Run("smesher ID mismatch", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATX),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(prevATX),
		)
		atx2.Sign(pubSig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.EqualError(t, err, "ATX2 is not a merged ATX, but NodeID is different from SmesherID")
		require.Nil(t, proof)

		proof, err = NewInvalidPrevAtxProofV2(db, atx1, atx2, pubSig.NodeID())
		require.EqualError(t, err, "ATX1 is not a merged ATX, but NodeID is different from SmesherID")
		require.Nil(t, proof)
	})

	t.Run("id not married to smesher", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		invalidSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		prevATXID := types.RandomATXID()
		prevAtx := &types.ActivationTx{}
		prevAtx.SetID(prevATXID)
		prevAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, prevAtx, types.AtxBlob{}))
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(invalidSig)
		atx2 := newMergedATXv2(db, prevATXID)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, invalidSig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("does not contain a marriage certificate signed by %s", invalidSig.NodeID().ShortString()),
		)
		require.Nil(t, proof)

		proof, err = NewInvalidPrevAtxProofV2(db, atx2, atx1, invalidSig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("does not contain a marriage certificate signed by %s", invalidSig.NodeID().ShortString()),
		)
		require.Nil(t, proof)
	})

	t.Run("merged ATX does not contain post from identity", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		prevAtx := &types.ActivationTx{}
		prevAtx.SetID(prevATXID)
		prevAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, prevAtx, types.AtxBlob{}))
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newMergedATXv2(db, prevATXID)

		// remove the post from sig in the merged ATX
		atx2.NIPosts[0].Posts = slices.DeleteFunc(atx2.NIPosts[0].Posts, func(subPost SubPostV2) bool {
			return subPost.MarriageIndex == 1
		})

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("no PoST from %s in ATX", sig.NodeID().ShortString()),
		)
		require.Nil(t, proof)

		proof, err = NewInvalidPrevAtxProofV2(db, atx2, atx1, sig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("no PoST from %s in ATX", sig.NodeID().ShortString()),
		)
		require.Nil(t, proof)
	})

	t.Run("prev ATX differs between ATXs", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		atx1 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
			withPublishEpoch(7),
		)
		atx2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.ErrorContains(t, err, "ATXs reference different previous ATXs")
		require.Nil(t, proof)
	})

	t.Run("invalid solo proof", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(7),
		)
		atx2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// same ATX ID
		proof.Proofs[0].ATXID = atx2.ID()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proofs have the same ATX ID")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].ATXID = atx1.ID()

		// invalid prev ATX
		proof.PrevATX = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid previous ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.PrevATX = prevATXID

		// invalid node ID
		proof.NodeID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "missing marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.NodeID = sig.NodeID()

		// invalid ATX ID
		proof.Proofs[0].ATXID = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].ATXID = atx1.ID()

		proof.Proofs[1].ATXID = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].ATXID = atx2.ID()

		// invalid SmesherID
		proof.Proofs[0].SmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].SmesherID = sig.NodeID()

		proof.Proofs[1].SmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].SmesherID = sig.NodeID()

		// invalid signature
		proof.Proofs[0].Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].Signature = atx1.Signature

		proof.Proofs[1].Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].Signature = atx2.Signature

		// invalid NIPosts root
		nipostsRoot := proof.Proofs[0].NIPostsRoot
		proof.Proofs[0].NIPostsRoot = NIPostsRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid NIPosts root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].NIPostsRoot = nipostsRoot

		nipostsRoot = proof.Proofs[1].NIPostsRoot
		proof.Proofs[1].NIPostsRoot = NIPostsRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid NIPosts root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].NIPostsRoot = nipostsRoot

		// invalid NIPosts root proof
		hash := proof.Proofs[0].NIPostsRootProof[0]
		proof.Proofs[0].NIPostsRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid NIPosts root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].NIPostsRootProof[0] = hash

		hash = proof.Proofs[1].NIPostsRootProof[0]
		proof.Proofs[1].NIPostsRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid NIPosts root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].NIPostsRootProof[0] = hash

		// invalid NIPost root
		nipostRoot := proof.Proofs[0].NIPostRoot
		proof.Proofs[0].NIPostRoot = NIPostRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].NIPostRoot = nipostRoot

		nipostRoot = proof.Proofs[1].NIPostRoot
		proof.Proofs[1].NIPostRoot = NIPostRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].NIPostRoot = nipostRoot

		// invalid NIPost root proof
		hash = proof.Proofs[0].NIPostRootProof[0]
		proof.Proofs[0].NIPostRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].NIPostRootProof[0] = hash

		hash = proof.Proofs[1].NIPostRootProof[0]
		proof.Proofs[1].NIPostRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].NIPostRootProof[0] = hash

		// invalid NIPost index
		proof.Proofs[0].NIPostIndex++
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].NIPostIndex--

		proof.Proofs[1].NIPostIndex++
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].NIPostIndex--

		// invalid sub posts root
		subPostsRoot := proof.Proofs[0].SubPostsRoot
		proof.Proofs[0].SubPostsRoot = SubPostsRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid sub PoSTs root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].SubPostsRoot = subPostsRoot

		subPostsRoot = proof.Proofs[1].SubPostsRoot
		proof.Proofs[1].SubPostsRoot = SubPostsRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid sub PoSTs root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].SubPostsRoot = subPostsRoot

		// invalid sub posts root proof
		hash = proof.Proofs[0].SubPostsRootProof[0]
		proof.Proofs[0].SubPostsRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid sub PoSTs root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].SubPostsRootProof[0] = hash

		hash = proof.Proofs[1].SubPostsRootProof[0]
		proof.Proofs[1].SubPostsRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid sub PoSTs root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].SubPostsRootProof[0] = hash

		// invalid sub post root
		subPostRoot := proof.Proofs[0].SubPostRoot
		proof.Proofs[0].SubPostRoot = SubPostRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].SubPostRoot = subPostRoot

		subPostRoot = proof.Proofs[1].SubPostRoot
		proof.Proofs[1].SubPostRoot = SubPostRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].SubPostRoot = subPostRoot

		// invalid sub post root proof
		hash = proof.Proofs[0].SubPostRootProof[0]
		proof.Proofs[0].SubPostRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].SubPostRootProof[0] = hash

		hash = proof.Proofs[1].SubPostRootProof[0]
		proof.Proofs[1].SubPostRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].SubPostRootProof[0] = hash

		// invalid sub post index
		proof.Proofs[0].SubPostRootIndex++
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].SubPostRootIndex--

		proof.Proofs[1].SubPostRootIndex++
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].SubPostRootIndex--

		// invalid prev atx proof
		hash = proof.Proofs[0].PrevATXProof[0]
		proof.Proofs[0].PrevATXProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid previous ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].PrevATXProof[0] = hash

		hash = proof.Proofs[1].PrevATXProof[0]
		proof.Proofs[1].PrevATXProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid previous ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].PrevATXProof[0] = hash
	})

	t.Run("invalid merged proof", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATXID := types.RandomATXID()
		prevAtx := &types.ActivationTx{}
		prevAtx.SetID(prevATXID)
		prevAtx.SmesherID = sig.NodeID()
		require.NoError(t, atxs.Add(db, prevAtx, types.AtxBlob{}))
		atx1 := newActivationTxV2(
			withPreviousATXs(prevATXID),
			withPublishEpoch(5),
		)
		atx1.Sign(sig)
		atx2 := newMergedATXv2(db, prevATXID)

		proof, err := NewInvalidPrevAtxProofV2(db, atx1, atx2, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// invalid node ID
		proof.NodeID = types.RandomNodeID()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "missing marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.NodeID = sig.NodeID()

		// invalid ATX ID
		proof.Proofs[0].ATXID = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].ATXID = atx1.ID()

		proof.Proofs[1].ATXID = types.RandomATXID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].ATXID = atx2.ID()

		// invalid SmesherID
		proof.Proofs[0].SmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].SmesherID = sig.NodeID()

		proof.Proofs[1].SmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].SmesherID = pubSig.NodeID()

		// invalid signature
		proof.Proofs[0].Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].Signature = atx1.Signature

		proof.Proofs[1].Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].Signature = atx2.Signature

		// missing marriage proof
		marriageProof := proof.Proofs[1].MarriageProof
		proof.Proofs[1].MarriageProof = nil
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "missing marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].MarriageProof = marriageProof

		// invalid marriage index proof
		hash := proof.Proofs[1].MarriageIndexProof[0]
		proof.Proofs[1].MarriageIndexProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid marriage index proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].MarriageIndexProof[0] = hash

		// invalid prev atx proof
		hash = proof.Proofs[0].PrevATXProof[0]
		proof.Proofs[0].PrevATXProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 1 is invalid: invalid previous ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[0].PrevATXProof[0] = hash

		hash = proof.Proofs[1].PrevATXProof[0]
		proof.Proofs[1].PrevATXProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "proof 2 is invalid: invalid previous ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Proofs[1].PrevATXProof[0] = hash
	})
}

func Test_InvalidPrevAtxProofV1(t *testing.T) {
	t.Parallel()

	// sig is the identity that creates the ATXs referencing the same prevATX
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// pubSig is the identity that publishes a merged ATX with the same prevATX
	pubSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// marrySig is the identity that publishes the marriage ATX
	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	newMergedATXv2 := func(
		db sql.Executor,
		prevATX types.ATXID,
	) *ActivationTxV2 {
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
		marriageAtx.SmesherID = marrySig.NodeID()
		require.NoError(t, atxs.Add(db, marriageAtx, wMarriageAtx.Blob()))

		atx := newActivationTxV2(
			withPreviousATXs(marryInitialAtx, wPubInitialAtx.ID(), prevATX),
			withMarriageATX(wMarriageAtx.ID()),
			withNIPost(
				withNIPostMembershipProof(MerkleProofV2{}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 0,
					PrevATXIndex:  0,
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 1,
					PrevATXIndex:  2,
				}),
				withNIPostSubPost(SubPostV2{
					MarriageIndex: 2,
					PrevATXIndex:  1,
				}),
			),
		)
		atx.Sign(pubSig)
		return atx
	}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(sig)

		atxv2 := newActivationTxV2(
			withPreviousATXs(prevATX),
			withPublishEpoch(7),
		)
		atxv2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// verify the proof
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("valid merged", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(sig)

		atxv2 := newMergedATXv2(db, prevATX)

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// verify the proof
		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("smesher ID mismatch", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(sig)

		atxv2 := newActivationTxV2(
			withPreviousATXs(prevATX),
			withPublishEpoch(7),
		)
		atxv2.Sign(pubSig)

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, pubSig.NodeID())
		require.EqualError(t, err, "ATX2 is not signed by NodeID")
		require.Nil(t, proof)

		proof, err = NewInvalidPrevAtxProofV1(db, atxv2, atxv1, sig.NodeID())
		require.EqualError(t, err, "ATX1 is not a merged ATX, but NodeID is different from SmesherID")
		require.Nil(t, proof)
	})

	t.Run("id not married to smesher", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		invalidSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(invalidSig)

		atxv2 := newMergedATXv2(db, prevATX)

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, invalidSig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("does not contain a marriage certificate signed by %s", invalidSig.NodeID().ShortString()),
		)
		require.Nil(t, proof)
	})

	t.Run("merged ATX does not contain post from identity", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(sig)

		atxv2 := newMergedATXv2(db, prevATX)

		// remove the post from sig in the merged ATX
		atxv2.NIPosts[0].Posts = slices.DeleteFunc(atxv2.NIPosts[0].Posts, func(subPost SubPostV2) bool {
			return subPost.MarriageIndex == 1
		})

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, sig.NodeID())
		require.ErrorContains(t, err,
			fmt.Sprintf("no PoST from %s in ATX", sig.NodeID().ShortString()),
		)
		require.Nil(t, proof)
	})

	t.Run("prev ATX differs between ATXs", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(sig)

		atxv2 := newActivationTxV2(
			withPreviousATXs(types.RandomATXID()),
			withPublishEpoch(7),
		)
		atxv2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, sig.NodeID())
		require.ErrorContains(t, err, "ATXs reference different previous ATXs")
		require.Nil(t, proof)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		prevATX := types.RandomATXID()
		atxv1 := &ActivationTxV1{
			InnerActivationTxV1: InnerActivationTxV1{
				NIPostChallengeV1: NIPostChallengeV1{
					PublishEpoch:     5,
					PrevATXID:        prevATX,
					PositioningATXID: types.RandomATXID(),
				},
			},
		}
		atxv1.Sign(sig)

		atxv2 := newActivationTxV2(
			withPreviousATXs(prevATX),
			withPublishEpoch(7),
		)
		atxv2.Sign(sig)

		proof, err := NewInvalidPrevAtxProofV1(db, atxv2, atxv1, sig.NodeID())
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// invalid PrevATX
		proof.PrevATX = types.RandomATXID()
		id, err := proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid previous ATX proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.PrevATX = prevATX

		// invalid SmesherID for atxv1
		proof.ATXv1.SmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXv1.SmesherID = sig.NodeID()

		// invalid signature for atxv1
		proof.ATXv1.Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid ATX signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXv1.Signature = atxv1.Signature

		// signer of atxv1 does not match
		proof.ATXv1.Sign(pubSig)
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "ATXv1 has not been signed by the same identity")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXv1.Sign(sig)

		// prevATX of atxv1 does not match
		proof.ATXv1.PrevATXID = types.RandomATXID()
		proof.ATXv1.Sign(sig)
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "ATXv1 references a different previous ATX")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXv1.PrevATXID = prevATX
		proof.ATXv1.Sign(sig)
	})
}
