package wire

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func Test_InvalidPostProof(t *testing.T) {
	t.Parallel()

	// sig is the identity that creates the invalid PoST
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// pubSig is the identity that publishes the merged ATX with the invalid PoST
	pubSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// marrySig is the identity that publishes the marriage ATX
	marrySig, err := signing.NewEdSigner()
	require.NoError(t, err)

	edVerifier := signing.NewEdVerifier()

	newSoloATXv2 := func(
		db sql.Executor,
		nipostChallenge types.Hash32,
		post PostV1,
		numUnits uint32,
	) *ActivationTxV2 {
		atx := newActivationTxV2(
			withNIPost(
				withNIPostChallenge(nipostChallenge),
				withNIPostSubPost(SubPostV2{
					Post:     post,
					NumUnits: numUnits,
				}),
			),
		)
		atx.Sign(sig)
		return atx
	}

	newMergedATXv2 := func(
		db sql.Executor,
		nipostChallenge types.Hash32,
		post PostV1,
		numUnits uint32,
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
					Post:          post,
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
		return atx
	}

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newSoloATXv2(db, nipostChallenge, post, numUnits)
		commitmentATX := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentATX, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
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
			commitmentATX,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			validPostIndex,
		).Return(nil)

		verifier.EXPECT().PostIndex(
			context.Background(),
			sig.NodeID(),
			commitmentATX,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			invalidPostIndex,
		).Return(errors.New("invalid post"))

		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("valid merged atx", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newMergedATXv2(db, nipostChallenge, post, numUnits)
		commitmentATX := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentATX, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
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
			commitmentATX,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			invalidPostIndex,
		).Return(errors.New("invalid post"))

		verifier.EXPECT().PostIndex(
			context.Background(),
			sig.NodeID(),
			commitmentATX,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			validPostIndex,
		).Return(nil)

		id, err := proof.Valid(context.Background(), verifier)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), id)
	})

	t.Run("post is valid", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newSoloATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
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
			commitmentAtx,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			validPostIndex,
		).Return(nil)

		verifier.EXPECT().PostIndex(
			context.Background(),
			sig.NodeID(),
			commitmentAtx,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			invalidPostIndex,
		).Return(nil)

		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid invalid post proof: PoST is valid")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("commitment ATX is not valid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newSoloATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
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
			commitmentAtx,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			validPostIndex,
		).Return(errors.New("invalid post"))

		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid invalid post proof: commitment ATX is not valid")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("differing node ID without marriage ATX", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newSoloATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		nodeID := types.RandomNodeID()
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, nodeID, 0, invalidPostIndex, validPostIndex)
		require.EqualError(t, err, "ATX is not a merged ATX, but NodeID is different from SmesherID")
		require.Nil(t, proof)
	})

	t.Run("nipost index is invalid", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newSoloATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 1, invalidPostIndex, validPostIndex)
		require.EqualError(t, err, "invalid NIPoST index")
		require.Nil(t, proof)
	})

	t.Run("node ID not in marriage ATX", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newMergedATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		nodeID := types.RandomNodeID()
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, nodeID, 0, invalidPostIndex, validPostIndex)
		require.ErrorContains(t, err,
			fmt.Sprintf("does not contain a marriage certificate signed by %s", nodeID.ShortString()),
		)
		require.Nil(t, proof)
	})

	t.Run("node ID did not include post in merged ATX", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newMergedATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()
		atx.NIPosts[0].Posts = slices.DeleteFunc(atx.NIPosts[0].Posts, func(subPost SubPostV2) bool {
			return cmp.Equal(subPost.Post, post)
		})

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
		require.EqualError(t, err, fmt.Sprintf("no PoST from %s in ATX", sig))
		require.Nil(t, proof)
	})

	t.Run("invalid solo proof", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newSoloATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// invalid ATXID
		proof.ATXID = types.RandomATXID()
		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXID = atx.ID()

		// invalid smesher ID
		proof.SmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.SmesherID = atx.SmesherID

		// invalid signature
		proof.Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Signature = atx.Signature

		// invalid node ID
		proof.NodeID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "missing marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.NodeID = sig.NodeID()

		// invalid niposts root
		nipostsRoot := proof.InvalidPostProof.NIPostsRoot
		proof.InvalidPostProof.NIPostsRoot = NIPostsRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid NIPosts root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NIPostsRoot = nipostsRoot

		// invalid niposts root proof
		hash := proof.InvalidPostProof.NIPostsRootProof[0]
		proof.InvalidPostProof.NIPostsRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid NIPosts root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NIPostsRootProof[0] = hash

		// invalid nipost root
		nipostRoot := proof.InvalidPostProof.NIPostRoot
		proof.InvalidPostProof.NIPostRoot = NIPostRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NIPostRoot = nipostRoot

		// invalid nipost root proof
		hash = proof.InvalidPostProof.NIPostRootProof[0]
		proof.InvalidPostProof.NIPostRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NIPostRootProof[0] = hash

		// invalid nipost index
		proof.InvalidPostProof.NIPostIndex = 1
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid NIPoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NIPostIndex = 0

		// invalid challenge
		challenge := proof.InvalidPostProof.Challenge
		proof.InvalidPostProof.Challenge = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid challenge proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.Challenge = challenge

		// invalid challenge proof
		hash = proof.InvalidPostProof.ChallengeProof[0]
		proof.InvalidPostProof.ChallengeProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid challenge proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.ChallengeProof[0] = hash

		// invalid subposts root
		subPostsRoot := proof.InvalidPostProof.SubPostsRoot
		proof.InvalidPostProof.SubPostsRoot = SubPostsRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid sub PoSTs root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.SubPostsRoot = subPostsRoot

		// invalid subposts root proof
		hash = proof.InvalidPostProof.SubPostsRootProof[0]
		proof.InvalidPostProof.SubPostsRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid sub PoSTs root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.SubPostsRootProof[0] = hash

		// invalid subpost root
		subPostRoot := proof.InvalidPostProof.SubPostRoot
		proof.InvalidPostProof.SubPostRoot = SubPostRoot(types.RandomHash())
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.SubPostRoot = subPostRoot

		// invalid subpost root proof
		hash = proof.InvalidPostProof.SubPostRootProof[0]
		proof.InvalidPostProof.SubPostRootProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.SubPostRootProof[0] = hash

		// invalid subpost root index
		proof.InvalidPostProof.SubPostRootIndex++
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid sub PoST root proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.SubPostRootIndex--

		// invalid post
		post = proof.InvalidPostProof.Post
		proof.InvalidPostProof.Post = PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid post proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.Post = post

		// invalid post proof
		hash = proof.InvalidPostProof.PostProof[0]
		proof.InvalidPostProof.PostProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid post proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.PostProof[0] = hash

		// invalid numunits
		proof.InvalidPostProof.NumUnits++
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid post proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NumUnits--

		// invalid numunits proof
		hash = proof.InvalidPostProof.NumUnitsProof[0]
		proof.InvalidPostProof.NumUnitsProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid post proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NumUnitsProof[0] = hash
	})

	t.Run("invalid merged proof", func(t *testing.T) {
		t.Parallel()
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newMergedATXv2(db, nipostChallenge, post, numUnits)
		commitmentAtx := types.RandomATXID()

		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
		require.NoError(t, err)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// invalid ATXID
		proof.ATXID = types.RandomATXID()
		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.ATXID = atx.ID()

		// invalid smesher ID
		proof.SmesherID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.SmesherID = atx.SmesherID

		// invalid signature
		proof.Signature = types.RandomEdSignature()
		id, err = proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
		proof.Signature = atx.Signature

		// invalid node ID
		proof.NodeID = types.RandomNodeID()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid marriage proof for NodeID")
		require.Equal(t, types.EmptyNodeID, id)
		proof.NodeID = sig.NodeID()

		// invalid marriage index proof
		hash := proof.InvalidPostProof.MarriageIndexProof[0]
		proof.InvalidPostProof.MarriageIndexProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid marriage index proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.MarriageIndexProof[0] = hash

		// invalid numunits proof
		hash = proof.InvalidPostProof.NumUnitsProof[0]
		proof.InvalidPostProof.NumUnitsProof[0] = types.RandomHash()
		id, err = proof.Valid(context.Background(), verifier)
		require.ErrorContains(t, err, "invalid post proof")
		require.Equal(t, types.EmptyNodeID, id)
		proof.InvalidPostProof.NumUnitsProof[0] = hash
	})
}
