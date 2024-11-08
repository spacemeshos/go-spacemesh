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
		require.EqualError(t, err, "invalid invalid post proof: Commitment ATX is not valid")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("differing node ID without marriage ATX", func(t *testing.T) {
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

		proof, err = NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 0, invalidPostIndex, validPostIndex)
		require.NoError(t, err)
		require.NotNil(t, proof)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		proof.NodeID = types.RandomNodeID() // invalid node ID

		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "missing marriage proof")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("node ID not in marriage ATX", func(t *testing.T) {
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

	t.Run("invalid marriage proof", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newMergedATXv2(db, nipostChallenge, post, numUnits)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// manually construct an invalid proof
		proof, err := createMarriageProof(db, atx, sig.NodeID())
		require.NoError(t, err)

		marriageATX := proof.MarriageATX
		proof.MarriageATX = types.RandomATXID() // invalid ATX
		err = proof.Valid(verifier, atx.ID(), sig.NodeID(), pubSig.NodeID())
		require.ErrorContains(t, err, "invalid marriage ATX proof")

		proof.MarriageATX = marriageATX
		proof.MarriageATXProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(verifier, atx.ID(), sig.NodeID(), pubSig.NodeID())
		require.ErrorContains(t, err, "invalid marriage ATX proof")
	})

	t.Run("node ID did not include post in merged ATX", func(t *testing.T) {
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

	t.Run("invalid nipost index", func(t *testing.T) {
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
		// 1 is an invalid nipostIndex for this ATX
		proof, err := NewInvalidPostProof(db, atx, commitmentAtx, sig.NodeID(), 1, invalidPostIndex, validPostIndex)
		require.EqualError(t, err, "invalid NIPoST index")
		require.Nil(t, proof)
	})

	t.Run("invalid ATX signature", func(t *testing.T) {
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

		proof.Signature = types.RandomEdSignature() // invalid signature

		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid signature")
		require.Equal(t, types.EmptyNodeID, id)
	})

	t.Run("solo invalid post proof is not valid", func(t *testing.T) {
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

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// manually construct an invalid proof
		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := createInvalidPostProof(atx, commitmentAtx, 0, 0, invalidPostIndex, validPostIndex)
		require.NoError(t, err)
		require.NotNil(t, proof)

		nipostsRoot := proof.NIPostsRoot
		proof.NIPostsRoot = NIPostsRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid NIPosts root proof")
		proof.NIPostsRoot = nipostsRoot

		proofHash := proof.NIPostsRootProof[0]
		proof.NIPostsRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid NIPosts root proof")
		proof.NIPostsRootProof[0] = proofHash

		proof.NIPostIndex = 1 // invalid index
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid NIPoST root proof")
		proof.NIPostIndex = 0

		nipostRoot := proof.NIPostRoot
		proof.NIPostRoot = NIPostRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid NIPoST root proof")
		proof.NIPostRoot = nipostRoot

		proofHash = proof.NIPostRootProof[0]
		proof.NIPostRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid NIPoST root proof")
		proof.NIPostRootProof[0] = proofHash

		challenge := proof.Challenge
		proof.Challenge = types.RandomHash() // invalid challenge
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid challenge proof")
		proof.Challenge = challenge

		proofHash = proof.ChallengeProof[0]
		proof.ChallengeProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid challenge proof")
		proof.ChallengeProof[0] = proofHash

		subPostsRoot := proof.SubPostsRoot
		proof.SubPostsRoot = SubPostsRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid sub PoSTs root proof")
		proof.SubPostsRoot = subPostsRoot

		proofHash = proof.SubPostsRootProof[0]
		proof.SubPostsRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid sub PoSTs root proof")
		proof.SubPostsRootProof[0] = proofHash

		proof.SubPostRootIndex = 1 // invalid index
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid sub PoST root proof")
		proof.SubPostRootIndex = 0

		subPost := proof.SubPostRoot
		proof.SubPostRoot = SubPostRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid sub PoST root proof")
		proof.SubPostRoot = subPost

		proofHash = proof.SubPostRootProof[0]
		proof.SubPostRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid sub PoST root proof")
		proof.SubPostRootProof[0] = proofHash

		proof.Post = PostV1{} // invalid post
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid PoST proof")
		proof.Post = post

		proof.NumUnits++ // invalid number of units
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), nil)
		require.EqualError(t, err, "invalid num units proof")
		proof.NumUnits--
	})

	t.Run("merged invalid post proof is not valid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx := newMergedATXv2(db, nipostChallenge, post, numUnits)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// manually construct an invalid proof
		marriageIndex := uint32(1)
		commitmentAtx := types.RandomATXID()
		const invalidPostIndex = 7
		const validPostIndex = 15
		proof, err := createInvalidPostProof(atx, commitmentAtx, 0, 1, invalidPostIndex, validPostIndex)
		require.NoError(t, err)
		require.NotNil(t, proof)

		invalidMarriageIndex := marriageIndex + 1

		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), &invalidMarriageIndex)
		require.EqualError(t, err, "invalid marriage index proof")
	})
}
