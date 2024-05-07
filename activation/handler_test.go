package activation

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacemeshos/merkle-tree"
	poetShared "github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	layersPerEpochBig = 1000
	localID           = "local"
)

func setID(atx *types.ActivationTx) {
	wireAtx := wire.ActivationTxToWireV1(atx)
	atx.SetID(types.ATXID(wireAtx.HashInnerBytes()))
}

func newMerkleProof(t testing.TB, leafs []types.Hash32) (types.MerkleProof, types.Hash32) {
	t.Helper()
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(poetShared.HashMembershipTreeNode).
		WithLeavesToProve(map[uint64]bool{0: true}).
		Build()
	require.NoError(t, err)
	for _, m := range leafs {
		require.NoError(t, tree.AddLeaf(m[:]))
	}
	root, nodes := tree.RootAndProof()
	nodesH32 := make([]types.Hash32, 0, len(nodes))
	for _, n := range nodes {
		nodesH32 = append(nodesH32, types.BytesToHash(n))
	}
	return types.MerkleProof{
		Nodes: nodesH32,
	}, types.BytesToHash(root)
}

func newNIPostWithPoet(t testing.TB, poetRef []byte) *nipost.NIPostState {
	t.Helper()
	proof, _ := newMerkleProof(t, []types.Hash32{
		types.BytesToHash([]byte("challenge")),
		types.BytesToHash([]byte("leaf2")),
		types.BytesToHash([]byte("leaf3")),
		types.BytesToHash([]byte("leaf4")),
	})

	return &nipost.NIPostState{
		NIPost: &types.NIPost{
			Membership: proof,
			Post: &types.Post{
				Nonce:   0,
				Indices: []byte{1, 2, 3},
				Pow:     0,
			},
			PostMetadata: &types.PostMetadata{
				Challenge: poetRef,
			},
		},
		NumUnits: 16,
		VRFNonce: 123,
	}
}

type testHandler struct {
	*Handler

	mclock     *MocklayerClock
	mpub       *pubsubmocks.MockPublisher
	mockFetch  *mocks.MockFetcher
	mValidator *MocknipostValidator
	mbeacon    *MockAtxReceiver
	mtortoise  *mocks.MockTortoise
}

func newTestHandler(tb testing.TB, goldenATXID types.ATXID) *testHandler {
	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	ctrl := gomock.NewController(tb)
	mclock := NewMocklayerClock(ctrl)
	mpub := pubsubmocks.NewMockPublisher(ctrl)
	mockFetch := mocks.NewMockFetcher(ctrl)
	mValidator := NewMocknipostValidator(ctrl)

	mbeacon := NewMockAtxReceiver(ctrl)
	mtortoise := mocks.NewMockTortoise(ctrl)

	atxHdlr := NewHandler(
		localID,
		cdb,
		atxsdata.New(),
		signing.NewEdVerifier(),
		mclock,
		mpub,
		mockFetch,
		1,
		goldenATXID,
		mValidator,
		mbeacon,
		mtortoise,
		lg,
	)
	return &testHandler{
		Handler: atxHdlr,

		mclock:     mclock,
		mpub:       mpub,
		mockFetch:  mockFetch,
		mValidator: mValidator,
		mbeacon:    mbeacon,
		mtortoise:  mtortoise,
	}
}

func TestHandler_SyntacticallyValidateAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	goldenATXID := types.ATXID{2, 3, 4}
	currentLayer := types.LayerID(1012)

	poetRef := []byte{0xba, 0xbe}
	npst := newNIPostWithPoet(t, poetRef)
	prevAtx := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		&goldenATXID,
		currentLayer.GetEpoch()-1,
		0,
		100,
		types.GenerateAddress([]byte("aaaa")),
		100,
		npst.NIPost,
	)
	nonce := types.VRFPostIndex(1)
	prevAtx.VRFNonce = &nonce
	posAtx := newActivationTx(
		t,
		otherSig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		&goldenATXID,
		currentLayer.GetEpoch()-1,
		0,
		100,
		types.GenerateAddress([]byte("bbbb")),
		100,
		npst.NIPost,
	)

	// Act & Assert
	t.Run("valid atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		_, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)
	})

	t.Run("valid atx with new VRF nonce", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		nonce := types.VRFPostIndex(999)
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		atx.VRFNonce = &nonce
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)

		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		_, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)
	})

	t.Run("valid atx with decreasing num units", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		// numunits decreased from 100 to 90 between atx and prevAtx
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 90, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		vAtx, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)
		require.Equal(t, uint32(90), vAtx.NumUnits)
		require.Equal(t, uint32(90), vAtx.EffectiveNumUnits())
	})

	t.Run("atx with increasing num units, no new VRF, old valid", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		// numunits increased from 100 to 110 between atx and prevAtx
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 110, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		vAtx, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)
		require.Equal(t, uint32(110), vAtx.NumUnits)
		require.Equal(t, uint32(100), vAtx.EffectiveNumUnits())
	})

	t.Run("atx with increasing num units, no new VRF, old invalid for new size", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		// numunits increased from 100 to 110 between atx and prevAtx
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 110, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("invalid VRF"))
		_, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.Nil(t, proof)
		require.ErrorContains(t, err, "invalid vrf nonce")
	})

	t.Run("valid initial atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, posAtx))

		ctxID := posAtx.ID()
		challenge := types.NIPostChallenge{
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: posAtx.ID(),
			CommitmentATX:  &ctxID,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
		atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		_, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)
	})

	t.Run("atx targeting wrong publish epoch", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch().Add(2),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "atx publish epoch is too far in the future")
	})

	t.Run("failing nipost challenge validation", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("nipost error"))
		_, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "nipost error")
		require.Nil(t, proof)
	})

	t.Run("failing positioning atx validation", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		atxHdlr.mValidator.EXPECT().
			PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("bad positioning atx"))
		_, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad positioning atx")
		require.Nil(t, proof)
	})

	t.Run("bad initial nipost challenge", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		cATX := posAtx.ID()
		challenge := types.NIPostChallenge{
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: posAtx.ID(),
			CommitmentATX:  &cATX,
		}
		atx := newAtx(challenge, npst.NIPost, npst.NumUnits, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		vrfNonce := types.VRFPostIndex(11)
		atx.VRFNonce = &vrfNonce
		atx.SmesherID = sig.NodeID()

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), sig.NodeID(), cATX, atx.InitialPost, gomock.Any(), atx.NumUnits, gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), cATX, &vrfNonce, gomock.Any(), atx.NumUnits)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("bad initial nipost"))
		_, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad initial nipost")
		require.Nil(t, proof)
	})

	t.Run("missing NodeID in initial atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		ctxID := posAtx.ID()
		challenge := types.NIPostChallenge{
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: posAtx.ID(),
			CommitmentATX:  &ctxID,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "node id is missing")
	})

	t.Run("missing VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		cATX := posAtx.ID()
		challenge := types.NIPostChallenge{
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: posAtx.ID(),
			CommitmentATX:  &cATX,
		}
		atx := newAtx(challenge, npst.NIPost, npst.NumUnits, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		nodeID := sig.NodeID()
		atx.InnerActivationTx.NodeID = &nodeID

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "vrf nonce is missing")
	})

	t.Run("invalid VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		ctxID := posAtx.ID()
		challenge := types.NIPostChallenge{
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: posAtx.ID(),
			CommitmentATX:  &ctxID,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("invalid VRF nonce"))
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF nonce")
	})

	t.Run("prevAtx not declared but initial Post not included", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := types.NIPostChallenge{
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: posAtx.ID(),
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.CommitmentATX = &goldenATXID

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err = atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but initial post is not included")
	})

	t.Run("prevAtx not declared but validation of initial post fails", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		cATX := posAtx.ID()
		challenge := types.NIPostChallenge{
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: posAtx.ID(),
			CommitmentATX:  &cATX,
		}
		atx := newAtx(challenge, npst.NIPost, npst.NumUnits, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		atx.VRFNonce = new(types.VRFPostIndex)
		*atx.VRFNonce = 1
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("failed post validation"))
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "failed post validation")
	})

	t.Run("prevAtx declared but initial Post is included", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.InitialPost = &types.Post{
			Nonce:   0,
			Indices: make([]byte, 10),
		}
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but initial post is included")
	})

	t.Run("prevAtx declared but NodeID is included", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, types.GenerateAddress([]byte("aaaa")))
		atx.NIPost = newNIPostWithPoet(t, poetRef).NIPost
		atx.InnerActivationTx.NodeID = new(types.NodeID)
		*atx.InnerActivationTx.NodeID = sig.NodeID()
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but node id is included")
	})
}

func TestHandler_ContextuallyValidateAtx(t *testing.T) {
	// Arrange
	layers := types.GetLayersPerEpoch()
	types.SetLayersPerEpoch(layersPerEpochBig)
	t.Cleanup(func() { types.SetLayersPerEpoch(layers) })

	goldenATXID := types.ATXID{2, 3, 4}
	currentLayer := types.LayerID(1012)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))
	poetRef := []byte{0x56, 0xbe}
	npst := newNIPostWithPoet(t, poetRef)
	prevAtx := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		currentLayer.GetEpoch()-1,
		0,
		100,
		coinbase,
		100,
		npst.NIPost,
	)

	// Act & Assert

	t.Run("valid atx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   0,
			PositioningATX: goldenATXID,
			CommitmentATX:  nil,
		}
		atx := newAtx(challenge, nil, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(sig, atx))

		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxHdlr.ContextuallyValidateAtx(vAtx))
	})

	t.Run("atx already exists", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		challenge := types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      types.EmptyATXID,
			PublishEpoch:   currentLayer.GetEpoch(),
			PositioningATX: goldenATXID,
			CommitmentATX:  nil,
		}
		nipost := types.NIPost{PostMetadata: &types.PostMetadata{}}
		atx := newAtx(challenge, &nipost, 100, coinbase)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(atxHdlr.cdb, vAtx))

		challenge = types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      prevAtx.ID(),
			PublishEpoch:   currentLayer.GetEpoch() - 1,
			PositioningATX: goldenATXID,
			CommitmentATX:  nil,
		}
		atx = newAtx(challenge, &nipost, 100, coinbase)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		vAtx, err = atx.Verify(0, 1)
		require.NoError(t, err)

		err = atxHdlr.ContextuallyValidateAtx(vAtx)
		require.EqualError(t, err, "last atx is not the one referenced")
	})

	t.Run("missing prevAtx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		challenge := types.NIPostChallenge{
			Sequence:       1,
			PrevATXID:      types.RandomATXID(),
			PublishEpoch:   0,
			PositioningATX: prevAtx.ID(),
			CommitmentATX:  nil,
		}
		atx := newAtx(challenge, nil, 2, types.Address{})
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)

		err = atxHdlr.ContextuallyValidateAtx(vAtx)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx by same node", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		vAtx := newActivationTx(
			t,
			sig,
			prevAtx.Sequence+1,
			prevAtx.ID(),
			prevAtx.ID(),
			nil,
			currentLayer.GetEpoch(),
			0,
			100,
			coinbase,
			100,
			&types.NIPost{PostMetadata: &types.PostMetadata{}},
		)
		require.NoError(t, atxs.Add(atxHdlr.cdb, vAtx))

		vAtx2 := newActivationTx(
			t,
			sig,
			vAtx.Sequence+1,
			prevAtx.ID(),
			prevAtx.ID(),
			nil,
			vAtx.PublishEpoch+1,
			0,
			100,
			coinbase,
			100,
			&types.NIPost{PostMetadata: &types.PostMetadata{}},
		)
		require.EqualError(t, atxHdlr.ContextuallyValidateAtx(vAtx2), "last atx is not the one referenced")

		id, err := atxs.GetLastIDByNodeID(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, vAtx.ID(), id)
		_, err = atxHdlr.cdb.GetAtxHeader(vAtx2.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx from different node", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)
		require.NoError(t, atxs.Add(atxHdlr.cdb, prevAtx))

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		otherAtx := newActivationTx(
			t,
			otherSig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			currentLayer.GetEpoch()-1,
			0,
			100,
			coinbase,
			100,
			npst.NIPost,
		)
		require.NoError(t, atxs.Add(atxHdlr.cdb, otherAtx))

		vAtx := newActivationTx(
			t,
			sig,
			2,
			otherAtx.ID(),
			otherAtx.ID(),
			nil,
			currentLayer.GetEpoch(),
			0,
			100,
			coinbase,
			100,
			&types.NIPost{PostMetadata: &types.PostMetadata{}},
		)
		require.EqualError(t, atxHdlr.ContextuallyValidateAtx(vAtx), "last atx is not the one referenced")

		id, err := atxs.GetLastIDByNodeID(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.Equal(t, prevAtx.ID(), id)
		_, err = atxHdlr.cdb.GetAtxHeader(vAtx.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
}

func TestHandler_ProcessAtx(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
		withVrfNonce(7),
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err := atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)

	// processing an already stored ATX returns no error
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)
}

func TestHandler_ProcessAtx_maliciousIdentity(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	require.NoError(t, identities.SetMalicious(atxHdlr.cdb, sig.NodeID(), types.RandomBytes(10), time.Now()))

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
		withVrfNonce(7),
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err := atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)
}

func TestHandler_ProcessAtx_SamePubEpoch(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
		withVrfNonce(7),
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err := atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)

	// another atx for the same epoch is considered malicious
	atx2 := newActivationTx(
		t,
		sig,
		1,
		atx1.ID(),
		atx1.ID(),
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
		withVrfNonce(7),
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx2)
	require.NoError(t, err)
	proof.SetReceived(time.Time{})
	nodeID, err := malfeasance.Validate(
		context.Background(),
		atxHdlr.log,
		atxHdlr.cdb,
		atxHdlr.edVerifier,
		nil,
		&mwire.MalfeasanceGossip{
			MalfeasanceProof: *proof,
		},
	)
	require.NoError(t, err)
	require.Equal(t, sig.NodeID(), nodeID)
}

func TestHandler_ProcessAtx_SamePubEpoch_NoSelfIncrimination(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atxHdlr.Register(sig)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
		withVrfNonce(7),
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err := atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)

	// processing an already stored ATX returns no error
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)

	// another atx for the same epoch is considered malicious
	atx2 := newActivationTx(
		t,
		sig,
		1,
		atx1.ID(),
		atx1.ID(),
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
	)
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx2)
	require.ErrorContains(t,
		err,
		fmt.Sprintf("%s already published an ATX", sig.NodeID().ShortString()),
	)
	require.Nil(t, proof) // no proof against oneself
}

func TestHandler_ProcessAtx_SamePrevATX(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	prevATX := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
		withVrfNonce(7),
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err := atxHdlr.processVerifiedATX(context.Background(), prevATX)
	require.NoError(t, err)
	require.Nil(t, proof)

	// valid first non-initial ATX
	atx1 := newActivationTx(
		t,
		sig,
		1,
		prevATX.ID(),
		prevATX.ID(),
		nil,
		types.EpochID(3),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)

	// second non-initial ATX references prevATX as prevATX
	atx2 := newActivationTx(
		t,
		sig,
		2,
		prevATX.ID(),
		atx1.ID(),
		nil,
		types.EpochID(4),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(types.EpochID(4).FirstLayer())
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx2)
	require.NoError(t, err)
	proof.SetReceived(time.Time{})
	nodeID, err := malfeasance.Validate(
		context.Background(),
		atxHdlr.log,
		atxHdlr.cdb,
		atxHdlr.edVerifier,
		nil,
		&mwire.MalfeasanceGossip{
			MalfeasanceProof: *proof,
		},
	)
	require.NoError(t, err)
	require.Equal(t, sig.NodeID(), nodeID)
}

func TestHandler_ProcessAtx_SamePrevATX_NoSelfIncrimination(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	atxHdlr.Register(sig)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	prevATX := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
		withVrfNonce(7),
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err := atxHdlr.processVerifiedATX(context.Background(), prevATX)
	require.NoError(t, err)
	require.Nil(t, proof)

	// valid first non-initial ATX
	atx1 := newActivationTx(
		t,
		sig,
		1,
		prevATX.ID(),
		prevATX.ID(),
		nil,
		types.EpochID(3),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
	)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)

	// second non-initial ATX references prevATX as prevATX
	atx2 := newActivationTx(
		t,
		sig,
		2,
		prevATX.ID(),
		atx1.ID(),
		nil,
		types.EpochID(4),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
	)
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx2)
	require.ErrorContains(t,
		err,
		fmt.Sprintf("%s referenced incorrect previous ATX", sig.NodeID().ShortString()),
	)
	require.Nil(t, proof) // no proof against oneself
}

func testHandler_PostMalfeasanceProofs(t *testing.T, synced bool) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID := sig.NodeID()

	proof, err := identities.GetMalfeasanceProof(atxHdlr.cdb, nodeID)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, proof)

	ch := types.NIPostChallenge{
		Sequence:       0,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   1,
		PositioningATX: goldenATXID,
		CommitmentATX:  &goldenATXID,
		InitialPost:    &types.Post{},
	}
	nipost := newNIPostWithPoet(t, []byte{0x76, 0x45})

	atx := newAtx(ch, nipost.NIPost, 100, types.GenerateAddress([]byte("aaaa")))
	atx.NodeID = &nodeID
	vrfNonce := types.VRFPostIndex(0)
	atx.VRFNonce = &vrfNonce
	atx.SetEffectiveNumUnits(100)
	atx.SetReceived(time.Now())
	require.NoError(t, SignAndFinalizeAtx(sig, atx))
	_, err = atx.Verify(0, 100)
	require.NoError(t, err)

	var got mwire.MalfeasanceGossip
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
	atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), goldenATXID, gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(&atx.NIPostChallenge, gomock.Any(), goldenATXID)
	atxHdlr.mValidator.EXPECT().PositioningAtx(goldenATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), goldenATXID, atx.NIPost, gomock.Any(), atx.NumUnits, gomock.Any()).
		Return(0, &verifying.ErrInvalidIndex{Index: 2})
	atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())
	if !synced {
		atxHdlr.mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, data []byte) error {
				require.NoError(t, codec.Decode(data, &got))
				postVerifier := NewMockPostVerifier(gomock.NewController(t))
				postVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(errors.New("invalid"))
				nodeID, err := malfeasance.Validate(
					context.Background(),
					atxHdlr.log,
					atxHdlr.cdb,
					atxHdlr.edVerifier,
					postVerifier,
					&got,
				)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				require.Equal(t, mwire.InvalidPostIndex, got.Proof.Type)
				p, ok := got.Proof.Data.(*mwire.InvalidPostIndexProof)
				require.True(t, ok)
				require.EqualValues(t, 2, p.InvalidIdx)
				return nil
			})
	}

	msg := codec.MustEncode(wire.ActivationTxToWireV1(atx))
	if synced {
		require.NoError(
			t, atxHdlr.HandleSyncedAtx(context.Background(), types.Hash32{}, p2p.NoPeer, msg))
	} else {
		require.ErrorIs(
			t, atxHdlr.HandleGossipAtx(context.Background(), p2p.NoPeer, msg),
			errMaliciousATX)
	}

	proof, err = identities.GetMalfeasanceProof(atxHdlr.cdb, atx.SmesherID)
	require.NoError(t, err)
	require.NotNil(t, proof.Received())
	proof.SetReceived(time.Time{})
	if !synced {
		require.Equal(t, got.MalfeasanceProof, *proof)
		require.Equal(t, atx.PublishEpoch.FirstLayer(), got.MalfeasanceProof.Layer)
	}
}

func TestHandler_PostMalfeasanceProofs(t *testing.T) {
	t.Run("produced but not published during sync", func(t *testing.T) {
		testHandler_PostMalfeasanceProofs(t, true)
	})

	t.Run("produced and published during gossip", func(t *testing.T) {
		testHandler_PostMalfeasanceProofs(t, false)
	})
}

func TestHandler_ProcessAtxStoresNewVRFNonce(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	coinbase := types.GenerateAddress([]byte("aaaa"))

	// Act & Assert
	atx1 := newActivationTx(
		t,
		sig,
		0,
		types.EmptyATXID,
		types.EmptyATXID,
		nil,
		types.EpochID(2),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
	)
	nonce1 := types.VRFPostIndex(123)
	atx1.VRFNonce = &nonce1
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err := atxHdlr.processVerifiedATX(context.Background(), atx1)
	require.NoError(t, err)
	require.Nil(t, proof)

	got, err := atxs.VRFNonce(atxHdlr.cdb, sig.NodeID(), atx1.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce1, got)

	// another atx for the same epoch is considered malicious
	atx2 := newActivationTx(
		t,
		sig,
		1,
		atx1.ID(),
		atx1.ID(),
		nil,
		types.EpochID(3),
		0,
		100,
		coinbase,
		100,
		&types.NIPost{PostMetadata: &types.PostMetadata{}},
	)
	nonce2 := types.VRFPostIndex(456)
	atx2.VRFNonce = &nonce2
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	proof, err = atxHdlr.processVerifiedATX(context.Background(), atx2)
	require.NoError(t, err)
	require.Nil(t, proof)

	got, err = atxs.VRFNonce(atxHdlr.cdb, sig.NodeID(), atx2.TargetEpoch())
	require.NoError(t, err)
	require.Equal(t, nonce2, got)
}

func BenchmarkNewActivationDb(b *testing.B) {
	r := require.New(b)

	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(b, goldenATXID)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())

	const (
		numOfMiners = 300
		batchSize   = 15
		numOfEpochs = 10 * batchSize
	)
	prevAtxs := make([]types.ATXID, numOfMiners)
	pPrevAtxs := make([]types.ATXID, numOfMiners)
	posAtx := types.ATXID{2, 3, 4}
	var atx *types.ActivationTx
	layer := types.LayerID(22)
	poetBytes := []byte("66666")
	coinbase := types.Address{2, 4, 5}
	sigs := make([]*signing.EdSigner, 0, numOfMiners)
	for range numOfMiners {
		sig, err := signing.NewEdSigner()
		r.NoError(err)
		sigs = append(sigs, sig)
	}
	r.Len(sigs, numOfMiners)

	start := time.Now()
	eStart := time.Now()
	for epoch := postGenesisEpoch; epoch < postGenesisEpoch+numOfEpochs; epoch++ {
		for i := range numOfMiners {
			challenge := types.NIPostChallenge{
				Sequence:       1,
				PrevATXID:      prevAtxs[i],
				PublishEpoch:   epoch,
				PositioningATX: posAtx,
				CommitmentATX:  nil,
			}
			npst := newNIPostWithPoet(b, poetBytes)
			atx = newAtx(challenge, npst.NIPost, npst.NumUnits, coinbase)
			SignAndFinalizeAtx(sigs[i], atx)
			vAtx, err := atx.Verify(0, 1)
			r.NoError(err)
			proof, err := atxHdlr.processVerifiedATX(context.Background(), vAtx)
			require.NoError(b, err)
			require.Nil(b, proof)
			prevAtxs[i] = atx.ID()
		}
		posAtx = atx.ID()
		layer = layer.Add(layersPerEpoch)
		if epoch%batchSize == batchSize-1 {
			b.Logf("epoch %3d-%3d took %v\t", epoch-(batchSize-1), epoch, time.Since(eStart))
			eStart = time.Now()

			for miner := 0; miner < numOfMiners; miner++ {
				atx, err := atxHdlr.cdb.GetAtxHeader(prevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
				atx, err = atxHdlr.cdb.GetAtxHeader(pPrevAtxs[miner])
				r.NoError(err)
				r.NotNil(atx)
			}
			b.Logf("reading last and previous epoch 100 times took %v\n", time.Since(eStart))
			eStart = time.Now()
		}
		copy(pPrevAtxs, prevAtxs)
	}
	b.Logf("\n>>> Total time: %v\n\n", time.Since(start))
}

func TestHandler_HandleGossipAtx(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID1 := sig1.NodeID()
	nipost := newNIPostWithPoet(t, []byte{0xba, 0xbe})
	vrfNonce := types.VRFPostIndex(12345)
	first := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   1,
				Sequence:       0,
				PrevATXID:      types.EmptyATXID,
				PositioningATX: goldenATXID,
				CommitmentATX:  &goldenATXID,
				InitialPost:    nipost.Post,
			},
			Coinbase: types.Address{2, 3, 4},
			NumUnits: 2,
			NIPost:   nipost.NIPost,
			NodeID:   &nodeID1,
			VRFNonce: &vrfNonce,
		},
		SmesherID: nodeID1,
	}
	setID(first)
	firstV1 := wire.ActivationTxToWireV1(first)
	firstV1.Signature = sig1.Sign(signing.ATX, firstV1.SignedBytes())

	second := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   2,
				Sequence:       1,
				PrevATXID:      first.ID(),
				PositioningATX: first.ID(),
			},
			Coinbase: types.Address{2, 3, 4},
			NumUnits: 2,
			NIPost:   nipost.NIPost,
		},
		SmesherID: nodeID1,
	}
	setID(second)
	secondV1 := wire.ActivationTxToWireV1(second)
	secondV1.Signature = sig1.Sign(signing.ATX, secondV1.SignedBytes())
	secondData, err := codec.Encode(secondV1)
	require.NoError(t, err)

	atxHdlr.mclock.EXPECT().CurrentLayer().Return(second.PublishEpoch.FirstLayer())
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), second.GetPoetProofRef()).Return(errors.New("woof"))
	err = atxHdlr.HandleGossipAtx(context.Background(), "", secondData)
	require.Error(t, err)
	require.ErrorContains(t, err, "missing poet proof")

	atxHdlr.mclock.EXPECT().CurrentLayer().Return(second.PublishEpoch.FirstLayer())
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), second.GetPoetProofRef())
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, ids []types.ATXID, _ ...system.GetAtxOpt) error {
			require.ElementsMatch(t, []types.ATXID{first.ID()}, ids)
			data, err := codec.Encode(firstV1)
			require.NoError(t, err)
			atxHdlr.mclock.EXPECT().CurrentLayer().Return(first.PublishEpoch.FirstLayer())
			atxHdlr.mValidator.EXPECT().
				Post(gomock.Any(), nodeID1, goldenATXID, first.InitialPost, gomock.Any(), first.NumUnits, gomock.Any())
			atxHdlr.mValidator.EXPECT().VRFNonce(nodeID1, goldenATXID, &vrfNonce, gomock.Any(), first.NumUnits)
			atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
			atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), first.GetPoetProofRef())
			atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(&first.NIPostChallenge, gomock.Any(), goldenATXID)
			atxHdlr.mValidator.EXPECT().PositioningAtx(goldenATXID, gomock.Any(), goldenATXID, first.PublishEpoch)
			atxHdlr.mValidator.EXPECT().
				NIPost(gomock.Any(), nodeID1, goldenATXID, second.NIPost, gomock.Any(), second.NumUnits, gomock.Any())
			atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
			atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
			require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", data))
			return nil
		},
	)
	atxHdlr.mValidator.EXPECT().NIPostChallenge(&second.NIPostChallenge, gomock.Any(), nodeID1)
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), nodeID1, goldenATXID, second.NIPost, gomock.Any(), second.NumUnits, gomock.Any())
	atxHdlr.mValidator.EXPECT().PositioningAtx(second.PositioningATX, gomock.Any(), goldenATXID, second.PublishEpoch)
	atxHdlr.mValidator.EXPECT().IsVerifyingFullPost().AnyTimes().Return(true)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", secondData))
}

func TestHandler_HandleParallelGossipAtxV1(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID := sig.NodeID()
	nipost := newNIPostWithPoet(t, []byte{0xba, 0xbe})
	vrfNonce := types.VRFPostIndex(12345)
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   1,
				PrevATXID:      types.EmptyATXID,
				PositioningATX: goldenATXID,
				CommitmentATX:  &goldenATXID,
				InitialPost:    nipost.Post,
			},
			Coinbase: types.Address{2, 3, 4},
			NumUnits: 2,
			NIPost:   nipost.NIPost,
			NodeID:   &nodeID,
			VRFNonce: &vrfNonce,
		},
		SmesherID: nodeID,
	}
	atxV1 := wire.ActivationTxToWireV1(atx)
	atxV1.Signature = sig.Sign(signing.ATX, atxV1.SignedBytes())
	atxData, err := codec.Encode(atxV1)
	require.NoError(t, err)

	atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
	atxHdlr.mValidator.EXPECT().VRFNonce(nodeID, goldenATXID, &vrfNonce, gomock.Any(), atx.NumUnits)
	atxHdlr.mValidator.EXPECT().
		Post(gomock.Any(), atx.SmesherID, goldenATXID, atx.InitialPost, gomock.Any(), atx.NumUnits).DoAndReturn(
		func(context.Context, types.NodeID, types.ATXID, *types.Post, *types.PostMetadata, uint32, ...validatorOption,
		) error {
			time.Sleep(100 * time.Millisecond)
			return nil
		},
	)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.GetPoetProofRef())
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(&atx.NIPostChallenge, gomock.Any(), goldenATXID)
	atxHdlr.mValidator.EXPECT().PositioningAtx(goldenATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), nodeID, goldenATXID, atx.NIPost, gomock.Any(), atx.NumUnits, gomock.Any())
	atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())

	var eg errgroup.Group
	for range 10 {
		eg.Go(func() error {
			return atxHdlr.HandleGossipAtx(context.Background(), "", atxData)
		})
	}

	require.NoError(t, eg.Wait())
}

func testHandler_HandleMaliciousAtx(t *testing.T, synced bool) {
	const (
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)
	goldenATXID := types.ATXID{2, 3, 4}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Parallel()

	atxHdlr := newTestHandler(t, goldenATXID)
	atxHdlr.tickSize = tickSize

	currentLayer := types.LayerID(100)

	nonce := types.VRFPostIndex(1)
	nodeId := sig.NodeID()
	atx1 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PositioningATX: goldenATXID,
				InitialPost:    &types.Post{Indices: []byte{1}},
				PublishEpoch:   1,
				CommitmentATX:  &goldenATXID,
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			VRFNonce: &nonce,
			NodeID:   &nodeId,
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx1))

	atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer).Times(2)
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any()).
		Return(leaves, nil).Times(2)
	atxHdlr.mValidator.EXPECT().
		PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).
		Times(2)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any()).Times(2)
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any()).Times(2)
	atxHdlr.mValidator.EXPECT().IsVerifyingFullPost().Return(true).Times(2)

	peer := p2p.Peer("buddy")
	proofRef := atx1.GetPoetProofRef()
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, []types.Hash32{proofRef})
	atxHdlr.mValidator.EXPECT().
		InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mValidator.EXPECT().
		VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	msg := codec.MustEncode(wire.ActivationTxToWireV1(atx1))
	require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), peer, msg))

	atx2 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				Sequence:       1,
				PositioningATX: atx1.ID(),
				PrevATXID:      atx1.ID(),
				PublishEpoch:   1,
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx2))

	proofRef = atx2.GetPoetProofRef()
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, gomock.Any()).Do(
		func(_ p2p.Peer, got []types.Hash32) {
			require.ElementsMatch(t, []types.Hash32{atx1.ID().Hash32(), proofRef}, got)
		})
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())

	var got mwire.MalfeasanceGossip
	if !synced {
		atxHdlr.mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, data []byte) error {
				require.NoError(t, codec.Decode(data, &got))
				nodeID, err := malfeasance.Validate(
					context.Background(),
					atxHdlr.log,
					atxHdlr.cdb,
					atxHdlr.edVerifier,
					nil,
					&got,
				)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				return nil
			})
	}

	msg = codec.MustEncode(wire.ActivationTxToWireV1(atx2))
	if synced {
		require.NoError(
			t, atxHdlr.HandleSyncedAtx(context.Background(), types.Hash32{}, peer, msg))
	} else {
		require.ErrorIs(
			t, atxHdlr.HandleGossipAtx(context.Background(), peer, msg),
			errMaliciousATX)
	}

	proof, err := identities.GetMalfeasanceProof(atxHdlr.cdb, sig.NodeID())
	require.NoError(t, err)
	require.NotNil(t, proof.Received())
	if !synced {
		proof.SetReceived(time.Time{})
		require.Equal(t, got.MalfeasanceProof, *proof)
	}
}

func TestHandler_HandleMaliciousAtx(t *testing.T) {
	t.Run("produced but not published during sync", func(t *testing.T) {
		testHandler_HandleMaliciousAtx(t, true)
	})

	t.Run("produced and published during gossip", func(t *testing.T) {
		testHandler_HandleMaliciousAtx(t, false)
	})
}

func TestHandler_HandleSyncedAtx(t *testing.T) {
	// Arrange
	goldenATXID := types.ATXID{2, 3, 4}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// Act & Assert

	t.Run("missing nipost", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			nil,
		)
		buf, err := codec.Encode(wire.ActivationTxToWireV1(atx.ActivationTx))
		require.NoError(t, err)

		require.ErrorContains(
			t,
			atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf),
			fmt.Sprintf("nil nipst for atx %v", atx.ID()),
		)
	})

	t.Run("known atx is ignored by handleAtxData", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			&types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			withVrfNonce(9),
		)

		atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
		proof, err := atxHdlr.processVerifiedATX(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)

		buf, err := codec.Encode(wire.ActivationTxToWireV1(atx.ActivationTx))
		require.NoError(t, err)

		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf))

		require.Error(t, atxHdlr.HandleGossipAtx(context.Background(), "", buf))
	})

	t.Run("known atx from local id is allowed", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			&types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			withVrfNonce(9),
		)

		atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
		proof, err := atxHdlr.processVerifiedATX(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)

		buf, err := codec.Encode(wire.ActivationTxToWireV1(atx.ActivationTx))
		require.NoError(t, err)
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf))
		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), localID, buf))
	})

	t.Run("atx with invalid signature", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newActivationTx(
			t,
			sig,
			0,
			types.EmptyATXID,
			types.EmptyATXID,
			nil,
			0,
			0,
			0,
			types.Address{2, 4, 5},
			2,
			&types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
		)
		atx.Signature[0] = ^atx.Signature[0] // fip first 8 bits
		buf, err := codec.Encode(wire.ActivationTxToWireV1(atx.ActivationTx))
		require.NoError(t, err)

		require.ErrorContains(
			t,
			atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf),
			"failed to verify atx signature",
		)
	})
}

func BenchmarkGetAtxHeaderWithConcurrentProcessAtx(b *testing.B) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(b, goldenATXID)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())

	var (
		stop uint64
		wg   sync.WaitGroup
	)
	for range runtime.NumCPU() / 2 {
		wg.Add(1)
		//nolint:testifylint
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				challenge := types.NIPostChallenge{
					Sequence:       uint64(i),
					PrevATXID:      types.EmptyATXID,
					PublishEpoch:   0,
					PositioningATX: goldenATXID,
					CommitmentATX:  nil,
				}
				sig, err := signing.NewEdSigner()
				require.NoError(b, err)
				atx := newAtx(challenge, nil, 1, types.Address{})
				require.NoError(b, SignAndFinalizeAtx(sig, atx))
				vAtx, err := atx.Verify(0, 1)
				if !assert.NoError(b, err) {
					return
				}
				proof, err := atxHdlr.processVerifiedATX(context.Background(), vAtx)
				if !assert.NoError(b, err) {
					return
				}
				if !assert.Nil(b, proof) {
					return
				}
				if atomic.LoadUint64(&stop) == 1 {
					return
				}
			}
		}()
	}
	b.ResetTimer()
	for range b.N {
		_, err := atxHdlr.cdb.GetAtxHeader(types.ATXID{1, 1, 1})
		require.ErrorIs(b, err, sql.ErrNotFound)
	}
	atomic.StoreUint64(&stop, 1)
	wg.Wait()
}

func TestCollectDeps(t *testing.T) {
	goldenATX := types.RandomATXID()
	atxA := types.RandomATXID()
	atxB := types.RandomATXID()
	atxC := types.RandomATXID()
	poet := types.RandomHash()

	t.Run("all unique deps", func(t *testing.T) {
		t.Parallel()
		atx := types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					PrevATXID:      atxA,
					PositioningATX: atxB,
					CommitmentATX:  &atxC,
				},
				NIPost: &types.NIPost{
					PostMetadata: &types.PostMetadata{
						Challenge: poet[:],
					},
				},
			},
		}
		poetDep, atxIDs := collectAtxDeps(goldenATX, &atx)
		require.Equal(t, poet, poetDep)
		require.ElementsMatch(t, []types.ATXID{atxA, atxB, atxC}, atxIDs)
	})
	t.Run("eliminates duplicates", func(t *testing.T) {
		t.Parallel()
		atx := types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					PrevATXID:      atxA,
					PositioningATX: atxA,
					CommitmentATX:  &atxA,
				},
				NIPost: &types.NIPost{
					PostMetadata: &types.PostMetadata{
						Challenge: poet[:],
					},
				},
			},
		}
		poetDep, atxIDs := collectAtxDeps(goldenATX, &atx)
		require.Equal(t, poet, poetDep)
		require.ElementsMatch(t, []types.ATXID{atxA}, atxIDs)
	})
	t.Run("nil commitment ATX", func(t *testing.T) {
		t.Parallel()
		atx := types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					PrevATXID:      atxA,
					PositioningATX: atxB,
					CommitmentATX:  nil,
				},
				NIPost: &types.NIPost{
					PostMetadata: &types.PostMetadata{
						Challenge: poet[:],
					},
				},
			},
		}
		poetDep, atxIDs := collectAtxDeps(goldenATX, &atx)
		require.Equal(t, poet, poetDep)
		require.ElementsMatch(t, []types.ATXID{atxA, atxB}, atxIDs)
	})
	t.Run("filters out golden ATX and empty ATX", func(t *testing.T) {
		t.Parallel()
		atx := types.ActivationTx{
			InnerActivationTx: types.InnerActivationTx{
				NIPostChallenge: types.NIPostChallenge{
					PrevATXID:      types.EmptyATXID,
					PositioningATX: goldenATX,
					CommitmentATX:  nil,
				},
				NIPost: &types.NIPost{
					PostMetadata: &types.PostMetadata{
						Challenge: poet[:],
					},
				},
			},
		}
		poetDep, atxIDs := collectAtxDeps(goldenATX, &atx)
		require.Equal(t, poet, poetDep)
		require.Empty(t, atxIDs)
	})
}

func TestHandler_RegistersHashesInPeer(t *testing.T) {
	goldenATXID := types.RandomATXID()
	peer := p2p.Peer("buddy")
	t.Run("registers poet and atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		poet := types.RandomHash()
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().
			RegisterPeerHashes(peer, gomock.InAnyOrder([]types.Hash32{poet, atxs[0].Hash32(), atxs[1].Hash32()}))
		atxHdlr.registerHashes(peer, poet, atxs)
	})
	t.Run("registers poet", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		poet := types.RandomHash()

		atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, []types.Hash32{poet})
		atxHdlr.registerHashes(peer, poet, nil)
	})
}

func TestHandler_FetchesReferences(t *testing.T) {
	goldenATXID := types.RandomATXID()
	t.Run("fetch poet and atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		poet := types.RandomHash()
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet)
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxs, gomock.Any())
		require.NoError(t, atxHdlr.fetchReferences(context.Background(), poet, atxs))
	})

	t.Run("no poet proofs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		poet := types.RandomHash()

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet).Return(errors.New("pooh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poet, nil))
	})

	t.Run("no atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, goldenATXID)

		poet := types.RandomHash()
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet).Return(errors.New("pooh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poet, nil))

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet)
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxs, gomock.Any()).Return(errors.New("oh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poet, atxs))
	})
}

func TestHandler_AtxWeight(t *testing.T) {
	const (
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)

	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)
	atxHdlr.tickSize = tickSize

	currentLayer := types.LayerID(100)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nonce := types.VRFPostIndex(1)
	nodeId := sig.NodeID()
	atx1 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PositioningATX: goldenATXID,
				InitialPost:    &types.Post{Indices: []byte{1}},
				PublishEpoch:   1,
				CommitmentATX:  &goldenATXID,
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			VRFNonce: &nonce,
			NodeID:   &nodeId,
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx1))

	buf, err := codec.Encode(wire.ActivationTxToWireV1(atx1))
	require.NoError(t, err)

	peer := p2p.Peer("buddy")
	proofRef := atx1.GetPoetProofRef()
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, []types.Hash32{proofRef})
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(leaves, nil)
	atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx1.ID().Hash32(), peer, buf))

	stored1, err := atxHdlr.cdb.GetAtxHeader(atx1.ID())
	require.NoError(t, err)
	require.Equal(t, uint64(0), stored1.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored1.TickCount)
	require.Equal(t, leaves/tickSize, stored1.TickHeight())
	require.Equal(t, (leaves/tickSize)*units, stored1.GetWeight())

	atx2 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				Sequence:       1,
				PositioningATX: atx1.ID(),
				PrevATXID:      atx1.ID(),
				PublishEpoch:   2,
			},
			NumUnits: units,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx2))

	buf, err = codec.Encode(wire.ActivationTxToWireV1(atx2))
	require.NoError(t, err)

	proofRef = atx2.GetPoetProofRef()
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, gomock.Any()).Do(
		func(_ p2p.Peer, got []types.Hash32) {
			require.ElementsMatch(t, []types.Hash32{atx1.ID().Hash32(), proofRef}, got)
		})
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(leaves, nil)
	atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
	atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mbeacon.EXPECT().OnAtx(gomock.Any())
	atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx2.ID().Hash32(), peer, buf))

	stored2, err := atxHdlr.cdb.GetAtxHeader(atx2.ID())
	require.NoError(t, err)
	require.Equal(t, stored1.TickHeight(), stored2.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored2.TickCount)
	require.Equal(t, stored1.TickHeight()+leaves/tickSize, stored2.TickHeight())
	require.Equal(t, int(leaves/tickSize)*units, int(stored2.GetWeight()))
}

func TestHandler_WrongHash(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)
	currentLayer := types.LayerID(100)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nonce := types.VRFPostIndex(1)
	nodeId := sig.NodeID()
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PositioningATX: goldenATXID,
				InitialPost:    &types.Post{Indices: []byte{1}},
				PublishEpoch:   1,
				CommitmentATX:  &goldenATXID,
			},
			NumUnits: 3,
			NIPost: &types.NIPost{
				Post:         &types.Post{},
				PostMetadata: &types.PostMetadata{},
			},
			VRFNonce: &nonce,
			NodeID:   &nodeId,
		},
	}
	require.NoError(t, SignAndFinalizeAtx(sig, atx))

	buf, err := codec.Encode(wire.ActivationTxToWireV1(atx))
	require.NoError(t, err)

	peer := p2p.Peer("buddy")
	proofRef := atx.GetPoetProofRef()
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(currentLayer)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, []types.Hash32{proofRef})
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), proofRef)
	atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(uint64(111), nil)
	atxHdlr.mValidator.EXPECT().IsVerifyingFullPost()
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	atxHdlr.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	err = atxHdlr.HandleSyncedAtx(context.Background(), types.RandomHash(), peer, buf)
	require.ErrorIs(t, err, errWrongHash)
	require.ErrorIs(t, err, pubsub.ErrValidationReject)
}

func TestHandler_MarksAtxValid(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	goldenATXID := types.ATXID{2, 3, 4}
	challenge := types.NIPostChallenge{
		Sequence:       0,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   2,
		PositioningATX: goldenATXID,
		CommitmentATX:  &goldenATXID,
	}
	nipost := newNIPostWithPoet(t, []byte("poet")).NIPost

	t.Run("post verified fully", func(t *testing.T) {
		t.Parallel()
		handler := newTestHandler(t, goldenATXID)
		handler.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any())
		handler.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		handler.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		handler.mValidator.EXPECT().IsVerifyingFullPost().Return(true)

		atx := newAtx(challenge, nipost, 2, types.Address{1, 2, 3, 4})
		atx.SetValidity(types.Unknown)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		_, proof, err := handler.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)
		require.Equal(t, types.Valid, atx.Validity())
	})
	t.Run("post verified fully", func(t *testing.T) {
		t.Parallel()
		handler := newTestHandler(t, goldenATXID)
		handler.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any())
		handler.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1), nil)
		handler.mValidator.EXPECT().PositioningAtx(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
		handler.mValidator.EXPECT().IsVerifyingFullPost().Return(false)

		atx := newAtx(challenge, nipost, 2, types.Address{1, 2, 3, 4})
		atx.SetValidity(types.Unknown)
		require.NoError(t, SignAndFinalizeAtx(sig, atx))
		_, proof, err := handler.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Nil(t, proof)
		require.Equal(t, types.Unknown, atx.Validity())
	})
	require.NoError(t, err)
}
