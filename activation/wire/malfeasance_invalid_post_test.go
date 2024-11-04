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
	) (*ActivationTxV2, *ActivationTxV2) {
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

		atx := newActivationTxV2(
			withPreviousATXs(wInitialAtx.ID()),
			withNIPost(
				withNIPostChallenge(nipostChallenge),
				withNIPostSubPost(SubPostV2{
					Post:     post,
					NumUnits: numUnits,
				}),
			),
		)
		atx.Sign(sig)
		return atx, wInitialAtx
	}

	newMergedATXv2 := func(
		db sql.Executor,
		nipostChallenge types.Hash32,
		post PostV1,
		numUnits uint32,
	) (*ActivationTxV2, *ActivationTxV2) {
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
		return atx, wInitialAtx
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
		atx, initialAtx := newSoloATXv2(db, nipostChallenge, post, numUnits)

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
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
			initialAtx.Initial.CommitmentATX,
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
		atx, initialAtx := newMergedATXv2(db, nipostChallenge, post, numUnits)

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
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
			initialAtx.Initial.CommitmentATX,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			invalidPostIndex,
		).Return(errors.New("invalid post"))

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
		atx, initialAtx := newSoloATXv2(db, nipostChallenge, post, numUnits)

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
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
			initialAtx.Initial.CommitmentATX,
			PostFromWireV1(&post),
			nipostChallenge.Bytes(),
			numUnits,
			invalidPostIndex,
		).Return(nil)

		id, err := proof.Valid(context.Background(), verifier)
		require.EqualError(t, err, "invalid invalid post proof: PoST is valid")
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
		atx, initialAtx := newSoloATXv2(db, nipostChallenge, post, numUnits)

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, types.RandomNodeID(), 0, invalidPostIndex)
		require.EqualError(t, err, "ATX is not a merged ATX, but NodeID is different from SmesherID")
		require.Nil(t, proof)

		proof, err = NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
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
		atx, initialAtx := newMergedATXv2(db, nipostChallenge, post, numUnits)

		const invalidPostIndex = 7
		nodeID := types.RandomNodeID()
		proof, err := NewInvalidPostProof(db, atx, initialAtx, nodeID, 0, invalidPostIndex)
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
		atx, _ := newMergedATXv2(db, nipostChallenge, post, numUnits)

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
		atx, initialAtx := newMergedATXv2(db, nipostChallenge, post, numUnits)
		atx.NIPosts[0].Posts = slices.DeleteFunc(atx.NIPosts[0].Posts, func(subPost SubPostV2) bool {
			return cmp.Equal(subPost.Post, post)
		})

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
		require.EqualError(t, err, fmt.Sprintf("no PoST from %s in ATX", sig))
		require.Nil(t, proof)
	})

	t.Run("initial ATX is invalid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		atx, initialAtx := newMergedATXv2(db, nipostChallenge, post, numUnits)
		initialAtx.SmesherID = types.RandomNodeID() // initial ATX published by different identity

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
		require.ErrorContains(t, err, "node ID does not match smesher ID of initial ATX")
		require.Nil(t, proof)

		atx, initialAtx = newMergedATXv2(db, nipostChallenge, post, numUnits)
		initialAtx.Initial = nil // not an initial ATX

		proof, err = NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
		require.ErrorContains(t, err, "initial ATX does not contain initial PoST")
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
		atx, initialAtx := newSoloATXv2(db, nipostChallenge, post, numUnits)

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 1, invalidPostIndex) // 1 is invalid
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
		atx, initialAtx := newSoloATXv2(db, nipostChallenge, post, numUnits)

		const invalidPostIndex = 7
		proof, err := NewInvalidPostProof(db, atx, initialAtx, sig.NodeID(), 0, invalidPostIndex)
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

	t.Run("commitment proof is invalid", func(t *testing.T) {
		db := statesql.InMemoryTest(t)

		nipostChallenge := types.RandomHash()
		const numUnits = uint32(11)
		post := PostV1{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(11),
			Pow:     rand.Uint64(),
		}
		_, initialAtx := newMergedATXv2(db, nipostChallenge, post, numUnits)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// manually construct an invalid proof
		proof, err := createCommitmentProof(initialAtx, sig.NodeID())
		require.NoError(t, err)

		signature := proof.Signature
		proof.Signature = types.RandomEdSignature() // invalid signature
		err = proof.Valid(verifier, sig.NodeID())
		require.ErrorContains(t, err, "invalid signature")
		proof.Signature = signature

		proof.InitialATXID = types.RandomATXID() // invalid ATX
		err = proof.Valid(verifier, sig.NodeID())
		require.ErrorContains(t, err, "invalid signature")
		proof.InitialATXID = initialAtx.ID()

		proofHash := proof.InitialPostProof[0]
		proof.InitialPostProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(verifier, sig.NodeID())
		require.ErrorContains(t, err, "invalid initial PoST proof")
		proof.InitialPostProof[0] = proofHash

		initialPostRoot := proof.InitialPostRoot
		proof.InitialPostRoot = InitialPostRoot(types.EmptyHash32) // invalid initial post root
		err = proof.Valid(verifier, sig.NodeID())
		require.ErrorContains(t, err, "invalid empty initial PoST root")
		proof.InitialPostRoot = initialPostRoot

		commitmentATX := proof.CommitmentATX
		proof.CommitmentATX = types.RandomATXID() // invalid ATX
		err = proof.Valid(verifier, sig.NodeID())
		require.ErrorContains(t, err, "invalid commitment ATX proof")
		proof.CommitmentATX = commitmentATX

		proofHash = proof.CommitmentATXProof[0]
		proof.CommitmentATXProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(verifier, sig.NodeID())
		require.ErrorContains(t, err, "invalid commitment ATX proof")
		proof.CommitmentATXProof[0] = proofHash
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
		atx, initialAtx := newSoloATXv2(db, nipostChallenge, post, numUnits)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// manually construct an invalid proof
		const invalidPostIndex = 7
		proof, err := createInvalidPostProof(atx, 0, 0, invalidPostIndex)
		require.NoError(t, err)
		require.NotNil(t, proof)

		nipostsRoot := proof.NIPostsRoot
		proof.NIPostsRoot = NIPostsRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid NIPosts root proof")
		proof.NIPostsRoot = nipostsRoot

		proofHash := proof.NIPostsRootProof[0]
		proof.NIPostsRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid NIPosts root proof")
		proof.NIPostsRootProof[0] = proofHash

		proof.NIPostIndex = 1 // invalid index
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid NIPoST root proof")
		proof.NIPostIndex = 0

		nipostRoot := proof.NIPostRoot
		proof.NIPostRoot = NIPostRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid NIPoST root proof")
		proof.NIPostRoot = nipostRoot

		proofHash = proof.NIPostRootProof[0]
		proof.NIPostRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid NIPoST root proof")
		proof.NIPostRootProof[0] = proofHash

		challenge := proof.Challenge
		proof.Challenge = types.RandomHash() // invalid challenge
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid challenge proof")
		proof.Challenge = challenge

		proofHash = proof.ChallengeProof[0]
		proof.ChallengeProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid challenge proof")
		proof.ChallengeProof[0] = proofHash

		subPostsRoot := proof.SubPostsRoot
		proof.SubPostsRoot = SubPostsRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid sub PoSTs root proof")
		proof.SubPostsRoot = subPostsRoot

		proofHash = proof.SubPostsRootProof[0]
		proof.SubPostsRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid sub PoSTs root proof")
		proof.SubPostsRootProof[0] = proofHash

		proof.SubPostRootIndex = 1 // invalid index
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid sub PoST root proof")
		proof.SubPostRootIndex = 0

		subPost := proof.SubPostRoot
		proof.SubPostRoot = SubPostRoot(types.RandomHash()) // invalid root
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid sub PoST root proof")
		proof.SubPostRoot = subPost

		proofHash = proof.SubPostRootProof[0]
		proof.SubPostRootProof[0] = types.RandomHash() // invalid proof
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid sub PoST root proof")
		proof.SubPostRootProof[0] = proofHash

		proof.Post = PostV1{} // invalid post
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
		require.EqualError(t, err, "invalid PoST proof")
		proof.Post = post

		proof.NumUnits++ // invalid number of units
		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), initialAtx.Initial.CommitmentATX, nil)
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
		atx, initialAtx := newMergedATXv2(db, nipostChallenge, post, numUnits)

		ctrl := gomock.NewController(t)
		verifier := NewMockMalfeasanceValidator(ctrl)
		verifier.EXPECT().Signature(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
				return edVerifier.Verify(d, nodeID, m, sig)
			}).AnyTimes()

		// manually construct an invalid proof
		marriageIndex := uint32(1)
		commitmentAtx := initialAtx.Initial.CommitmentATX
		const invalidPostIndex = 7
		proof, err := createInvalidPostProof(atx, 0, 1, invalidPostIndex)
		require.NoError(t, err)
		require.NotNil(t, proof)

		invalidMarriageIndex := marriageIndex + 1

		err = proof.Valid(context.Background(), verifier, atx.ID(), sig.NodeID(), commitmentAtx, &invalidMarriageIndex)
		require.EqualError(t, err, "invalid marriage index proof")
	})
}
