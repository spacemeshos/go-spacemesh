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
	"github.com/spacemeshos/post/shared"
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
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const layersPerEpochBig = 1000

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

func newNIPosV1tWithPoet(t testing.TB, poetRef []byte) *wire.NIPostV1 {
	t.Helper()
	proof, _ := newMerkleProof(t, []types.Hash32{
		types.BytesToHash([]byte("challenge")),
		types.BytesToHash([]byte("leaf2")),
		types.BytesToHash([]byte("leaf3")),
		types.BytesToHash([]byte("leaf4")),
	})

	return &wire.NIPostV1{
		Membership: wire.MerkleProofV1{
			Nodes:     proof.Nodes,
			LeafIndex: 0,
		},
		Post: &wire.PostV1{
			Nonce:   0,
			Indices: []byte{1, 2, 3},
			Pow:     0,
		},
		PostMetadata: &wire.PostMetadataV1{
			Challenge: poetRef,
		},
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

type atxHandleOpts struct {
	postVerificationDuration time.Duration
	poetLeaves               uint64
	distributedPost          bool
}

func (h *testHandler) expectAtxV1(atx *wire.ActivationTxV1, nodeId types.NodeID, opts ...func(*atxHandleOpts)) {
	settings := atxHandleOpts{}
	for _, opt := range opts {
		opt(&settings)
	}
	h.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())

	if atx.VRFNonce != nil {
		h.mValidator.EXPECT().VRFNonce(nodeId, h.goldenATXID, *atx.VRFNonce, atx.NumUnits)
	}
	h.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	h.mockFetch.EXPECT().GetPoetProof(gomock.Any(), types.BytesToHash(atx.NIPost.PostMetadata.Challenge))
	if atx.PrevATXID == types.EmptyATXID {
		h.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), h.goldenATXID)
		h.mValidator.EXPECT().
			Post(gomock.Any(), nodeId, h.goldenATXID, gomock.Any(), shared.ZeroChallenge, atx.NumUnits, gomock.Any()).
			DoAndReturn(func(
				_ context.Context, _ types.NodeID, _ types.ATXID, _ *types.Post,
				_ []byte, _ uint32, _ ...validatorOption,
			) error {
				time.Sleep(settings.postVerificationDuration)
				return nil
			})
	} else {
		h.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), nodeId)
	}
	h.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), h.goldenATXID, atx.PublishEpoch)
	h.mValidator.EXPECT().
		NIPost(gomock.Any(), nodeId, h.goldenATXID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
		Return(settings.poetLeaves, nil)
	h.mValidator.EXPECT().IsVerifyingFullPost().Return(!settings.distributedPost)
	h.mbeacon.EXPECT().OnAtx(gomock.Any())
	h.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
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
		"localID",
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
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	goldenATXID := types.RandomATXID()

	setup := func(t *testing.T) (hdlr *testHandler, prev, pos *wire.ActivationTxV1) {
		atxHdlr := newTestHandler(t, goldenATXID)

		prevAtx := newActivationTxV1(t, goldenATXID)
		prevAtx.NumUnits = 100
		prevAtx.Sign(sig)
		atxHdlr.expectAtxV1(prevAtx, sig.NodeID())
		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(prevAtx)))

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		posAtx := newActivationTxV1(t, goldenATXID)
		posAtx.Sign(otherSig)
		atxHdlr.expectAtxV1(posAtx, otherSig.NodeID())
		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(posAtx)))
		return atxHdlr, prevAtx, posAtx
	}

	t.Run("valid atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, atxHdlr.goldenATXID, prevAtx, posAtx.ID())
		atx.PositioningATXID = posAtx.ID()
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), atx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(1234, nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), atx.SmesherID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, gomock.Any())
		leaves, units, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Equal(t, uint64(1234), leaves)
		require.Equal(t, atx.NumUnits, units)
		require.Nil(t, proof)
	})

	t.Run("valid atx with new VRF nonce", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		newNonce := *prevAtx.VRFNonce + 100
		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.VRFNonce = &newNonce
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(1234, nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), goldenATXID, newNonce, atx.NumUnits)
		leaves, units, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Equal(t, uint64(1234), leaves)
		require.Equal(t, atx.NumUnits, units)
		require.Nil(t, proof)
	})

	t.Run("valid atx with decreasing num units", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.NumUnits = prevAtx.NumUnits - 10
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1234), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		leaves, units, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Equal(t, uint64(1234), leaves)
		require.Equal(t, atx.NumUnits, units)
		require.Nil(t, proof)
	})

	t.Run("atx with increasing num units, no new VRF, old valid", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.NumUnits = prevAtx.NumUnits + 10
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1234), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), goldenATXID, *prevAtx.VRFNonce, atx.NumUnits)
		leaves, units, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Equal(t, uint64(1234), leaves)
		require.Equal(t, prevAtx.NumUnits, units)
		require.Nil(t, proof)
	})

	t.Run("atx with increasing num units, no new VRF, old invalid for new size", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.NumUnits = prevAtx.NumUnits + 10
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), goldenATXID, *prevAtx.VRFNonce, atx.NumUnits).
			Return(errors.New("invalid VRF"))
		_, _, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF")
		require.Nil(t, proof)
	})

	t.Run("valid initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, posAtx := setup(t)

		ctxID := posAtx.ID()
		atx := newActivationTxV1(t, goldenATXID)
		atx.CommitmentATXID = &ctxID
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), ctxID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), ctxID, *atx.VRFNonce, atx.NumUnits)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), goldenATXID)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(777), nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		leaves, units, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.NoError(t, err)
		require.Equal(t, uint64(777), leaves)
		require.Equal(t, atx.NumUnits, units)
		require.Nil(t, proof)
	})

	t.Run("atx targeting wrong publish epoch", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return((atx.PublishEpoch - 2).FirstLayer())
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "atx publish epoch is too far in the future")
	})

	t.Run("failing nipost challenge validation", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			NIPostChallenge(gomock.Any(), gomock.Any(), atx.SmesherID).
			Return(errors.New("nipost error"))
		_, _, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "nipost error")
		require.Nil(t, proof)
	})

	t.Run("failing positioning atx validation", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().NIPostChallenge(gomock.Any(), gomock.Any(), atx.SmesherID)
		atxHdlr.mValidator.EXPECT().
			PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch).
			Return(errors.New("bad positioning atx"))
		_, _, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad positioning atx")
		require.Nil(t, proof)
	})

	t.Run("bad initial nipost challenge", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, posAtx := setup(t)

		cATX := posAtx.ID()
		atx := newActivationTxV1(t, goldenATXID)
		atx.CommitmentATXID = &cATX
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), sig.NodeID(), cATX, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), cATX, *atx.VRFNonce, atx.NumUnits)
		require.NoError(t, atxHdlr.SyntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			InitialNIPostChallenge(gomock.Any(), gomock.Any(), goldenATXID).
			Return(errors.New("bad initial nipost"))
		_, _, proof, err := atxHdlr.SyntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad initial nipost")
		require.Nil(t, proof)
	})

	t.Run("missing NodeID in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newActivationTxV1(t, goldenATXID)
		atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())
		atx.SmesherID = sig.NodeID()

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "node id is missing")
	})

	t.Run("missing VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newActivationTxV1(t, goldenATXID)
		atx.VRFNonce = nil
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "vrf nonce is missing")
	})

	t.Run("invalid VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newActivationTxV1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			VRFNonce(atx.SmesherID, *atx.CommitmentATXID, *atx.VRFNonce, atx.NumUnits).
			Return(errors.New("invalid VRF nonce"))
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF nonce")
	})

	t.Run("prevAtx not declared but initial Post not included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.PrevATXID = types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err = atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but initial post is not included")
	})

	t.Run("prevAtx not declared but validation of initial post fails", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newActivationTxV1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().VRFNonce(atx.SmesherID, *atx.CommitmentATXID, *atx.VRFNonce, atx.NumUnits)
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), atx.SmesherID, gomock.Any(), gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
			Return(errors.New("failed post validation"))
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "failed post validation")
	})

	t.Run("prevAtx declared but initial Post is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, _ := setup(t)

		atx := newActivationTxV1(t, goldenATXID)
		atx.PrevATXID = prevAtx.ID()
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.SyntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but initial post is included")
	})

	t.Run("prevAtx declared but NodeID is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.NodeID = &types.NodeID{1, 2, 3}
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
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
		types.LayerID(layersPerEpoch).GetEpoch(),
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
		types.LayerID(layersPerEpoch+1).GetEpoch(),
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

func TestHandler_ProcessAtx_OwnNotMalicious(t *testing.T) {
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
		types.LayerID(layersPerEpoch).GetEpoch(),
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
		types.LayerID(layersPerEpoch+1).GetEpoch(),
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
	require.Nil(t, proof)
}

func testHandler_PostMalfeasanceProofs(t *testing.T, synced bool) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID := sig.NodeID()

	_, err = identities.GetMalfeasanceProof(atxHdlr.cdb, nodeID)
	require.ErrorIs(t, err, sql.ErrNotFound)

	atx := newActivationTxV1(t, goldenATXID)
	atx.Sign(sig)

	var got mwire.MalfeasanceGossip
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
	atxHdlr.mValidator.EXPECT().VRFNonce(atx.SmesherID, goldenATXID, *atx.VRFNonce, atx.NumUnits)
	atxHdlr.mValidator.EXPECT().
		Post(gomock.Any(), gomock.Any(), *atx.CommitmentATXID, gomock.Any(), gomock.Any(), atx.NumUnits)
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
	atxHdlr.mValidator.EXPECT().InitialNIPostChallenge(gomock.Any(), gomock.Any(), goldenATXID)
	atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
	atxHdlr.mValidator.EXPECT().
		NIPost(gomock.Any(), atx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
		Return(0, &verifying.ErrInvalidIndex{Index: 2})
	atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())

	msg := codec.MustEncode(atx)
	if synced {
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), types.Hash32{}, p2p.NoPeer, msg))
	} else {
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
		require.ErrorIs(t, atxHdlr.HandleGossipAtx(context.Background(), p2p.NoPeer, msg), errMaliciousATX)
	}

	proof, err := identities.GetMalfeasanceProof(atxHdlr.cdb, atx.SmesherID)
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
		types.LayerID(layersPerEpoch).GetEpoch(),
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
		types.LayerID(2*layersPerEpoch).GetEpoch(),
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

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	first := newActivationTxV1(t, goldenATXID)
	first.Sign(sig)

	second := newChainedActivationTxV1(t, goldenATXID, first, first.ID())
	second.Sign(sig)

	// the poet is missing
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(second.PublishEpoch.FirstLayer())
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(
		p2p.NoPeer,
		[]types.Hash32{first.ID().Hash32(), types.Hash32(second.NIPost.PostMetadata.Challenge)},
	)
	atxHdlr.mockFetch.EXPECT().
		GetPoetProof(gomock.Any(), types.Hash32(second.NIPost.PostMetadata.Challenge)).
		Return(errors.New("missing poet proof"))

	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(second))
	require.ErrorContains(t, err, "missing poet proof")

	// deps (prevATX, posATX, commitmentATX) are missing
	atxHdlr.mclock.EXPECT().CurrentLayer().Return(second.PublishEpoch.FirstLayer())
	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(
		p2p.NoPeer,
		[]types.Hash32{first.ID().Hash32(), types.Hash32(second.NIPost.PostMetadata.Challenge)},
	)
	atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), types.Hash32(second.NIPost.PostMetadata.Challenge))
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{second.PrevATXID}, gomock.Any())
	err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(second))
	require.ErrorContains(t, err, "syntactically invalid based on deps")

	// valid first comes in
	atxHdlr.expectAtxV1(first, sig.NodeID())
	require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(first)))

	// second is now valid (deps are in)
	atxHdlr.expectAtxV1(second, sig.NodeID())
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{second.PrevATXID}, gomock.Any())
	require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(second)))
}

func TestHandler_HandleParallelGossipAtxV1(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1 := newActivationTxV1(t, goldenATXID)
	atx1.Sign(sig)
	atxHdlr.expectAtxV1(
		atx1,
		sig.NodeID(),
		func(o *atxHandleOpts) { o.postVerificationDuration = time.Millisecond * 100 },
	)

	var eg errgroup.Group
	for range 10 {
		eg.Go(func() error {
			return atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx1))
		})
	}

	require.NoError(t, eg.Wait())
}

func testHandler_HandleMaliciousAtx(t *testing.T, synced bool) {
	t.Parallel()
	goldenATXID := types.ATXID{2, 3, 4}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	hdlr := newTestHandler(t, goldenATXID)

	atx1 := newActivationTxV1(t, goldenATXID)
	atx1.Sign(sig)
	hdlr.expectAtxV1(atx1, sig.NodeID())
	require.NoError(t, hdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx1)))

	atx2 := newActivationTxV1(t, goldenATXID, func(a *wire.ActivationTxV1) { a.NumUnits = atx1.NumUnits + 1 })
	atx2.Sign(sig)
	hdlr.expectAtxV1(atx2, sig.NodeID())
	hdlr.mtortoise.EXPECT().OnMalfeasance(sig.NodeID())

	msg := codec.MustEncode(atx2)
	var got mwire.MalfeasanceGossip
	if synced {
		require.NoError(t, hdlr.HandleSyncedAtx(context.Background(), types.Hash32{}, "", msg))
	} else {
		hdlr.mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, data []byte) error {
				require.NoError(t, codec.Decode(data, &got))
				nodeID, err := malfeasance.Validate(
					context.Background(),
					hdlr.log,
					hdlr.cdb,
					hdlr.edVerifier,
					nil,
					&got,
				)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				return nil
			})
		require.ErrorIs(t, hdlr.HandleGossipAtx(context.Background(), p2p.NoPeer, msg), errMaliciousATX)
	}

	proof, err := identities.GetMalfeasanceProof(hdlr.cdb, sig.NodeID())
	require.NoError(t, err)
	require.NotNil(t, proof)
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
	goldenATXID := types.RandomATXID()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("missing nipost", func(t *testing.T) {
		t.Parallel()

		atx := newActivationTxV1(t, goldenATXID, func(a *wire.ActivationTxV1) { a.NIPost = nil })
		atx.Sign(sig)
		buf := codec.MustEncode(atx)

		atxHdlr := newTestHandler(t, goldenATXID)
		require.ErrorContains(
			t,
			atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf),
			fmt.Sprintf("nil nipost for atx %v", atx.ID()),
		)
	})

	t.Run("known atx is ignored by HandleSyncedAtx", func(t *testing.T) {
		t.Parallel()

		atx := newActivationTxV1(t, goldenATXID)
		atx.Sign(sig)
		buf := codec.MustEncode(atx)

		atxHdlr := newTestHandler(t, goldenATXID)
		atxHdlr.expectAtxV1(atx, sig.NodeID())

		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", buf))
		require.Error(t, atxHdlr.HandleGossipAtx(context.Background(), "", buf))
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf))
	})

	t.Run("known atx from local id is allowed", func(t *testing.T) {
		t.Parallel()

		atx := newActivationTxV1(t, goldenATXID)
		atx.Sign(sig)
		buf := codec.MustEncode(atx)

		atxHdlr := newTestHandler(t, goldenATXID)
		atxHdlr.expectAtxV1(atx, sig.NodeID())

		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), p2p.NoPeer, buf))
		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), atxHdlr.local, buf))
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf))
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), atxHdlr.local, buf))
	})

	t.Run("atx with invalid signature", func(t *testing.T) {
		t.Parallel()

		atx := newActivationTxV1(t, goldenATXID)
		atx.Sign(sig)
		atx.Signature = types.RandomEdSignature()
		buf := codec.MustEncode(atx)

		atxHdlr := newTestHandler(t, goldenATXID)
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
		atx := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PrevATXID:        atxA,
					PositioningATXID: atxB,
					CommitmentATXID:  &atxC,
				},
				NIPost: &wire.NIPostV1{
					PostMetadata: &wire.PostMetadataV1{
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
		atx := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PrevATXID:        atxA,
					PositioningATXID: atxA,
					CommitmentATXID:  &atxA,
				},
				NIPost: &wire.NIPostV1{
					PostMetadata: &wire.PostMetadataV1{
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
		atx := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PrevATXID:        atxA,
					PositioningATXID: atxB,
				},
				NIPost: &wire.NIPostV1{
					PostMetadata: &wire.PostMetadataV1{
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
		atx := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: wire.NIPostChallengeV1{
					PrevATXID:        types.EmptyATXID,
					PositioningATXID: goldenATX,
				},
				NIPost: &wire.NIPostV1{
					PostMetadata: &wire.PostMetadataV1{
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
		peer     = p2p.Peer("buddy")
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)

	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)
	atxHdlr.tickSize = tickSize

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1 := newActivationTxV1(t, goldenATXID)
	atx1.NumUnits = units
	atx1.Sign(sig)
	buf := codec.MustEncode(atx1)

	atxHdlr.expectAtxV1(atx1, sig.NodeID(), func(o *atxHandleOpts) { o.poetLeaves = leaves })
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx1.ID().Hash32(), peer, buf))

	stored1, err := atxHdlr.cdb.GetAtxHeader(atx1.ID())
	require.NoError(t, err)
	require.Equal(t, uint64(0), stored1.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored1.TickCount)
	require.Equal(t, leaves/tickSize, stored1.TickHeight())
	require.Equal(t, (leaves/tickSize)*units, stored1.GetWeight())

	atx2 := newChainedActivationTxV1(t, goldenATXID, atx1, atx1.ID())
	atx2.Sign(sig)
	buf = codec.MustEncode(atx2)

	atxHdlr.expectAtxV1(atx2, sig.NodeID(), func(o *atxHandleOpts) { o.poetLeaves = leaves })
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx1.ID()}, gomock.Any())
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx2.ID().Hash32(), peer, buf))

	stored2, err := atxHdlr.cdb.GetAtxHeader(atx2.ID())
	require.NoError(t, err)
	require.Equal(t, stored1.TickHeight(), stored2.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored2.TickCount)
	require.Equal(t, stored1.TickHeight()+leaves/tickSize, stored2.TickHeight())
	require.Equal(t, int(leaves/tickSize)*units, int(stored2.GetWeight()))
}

func TestHandler_WrongHash(t *testing.T) {
	goldenATXID := types.RandomATXID()
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx := newActivationTxV1(t, goldenATXID)
	atx.Sign(sig)

	err = atxHdlr.HandleSyncedAtx(context.Background(), types.RandomHash(), "", codec.MustEncode(atx))
	require.ErrorIs(t, err, errWrongHash)
	require.ErrorIs(t, err, pubsub.ErrValidationReject)
}

func TestHandler_MarksAtxValid(t *testing.T) {
	t.Parallel()
	goldenATXID := types.RandomATXID()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("post verified fully", func(t *testing.T) {
		t.Parallel()

		atx := newActivationTxV1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr := newTestHandler(t, goldenATXID)
		atxHdlr.expectAtxV1(atx, sig.NodeID(), func(o *atxHandleOpts) { o.distributedPost = false })
		err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx))

		vatx, err := atxs.Get(atxHdlr.cdb, atx.ID())
		require.NoError(t, err)
		require.Equal(t, types.Valid, vatx.Validity())
	})
	t.Run("post not verified fully (distributed post)", func(t *testing.T) {
		t.Parallel()

		atx := newActivationTxV1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr := newTestHandler(t, goldenATXID)
		atxHdlr.expectAtxV1(atx, sig.NodeID(), func(o *atxHandleOpts) { o.distributedPost = true })
		err = atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx))

		vatx, err := atxs.Get(atxHdlr.cdb, atx.ID())
		require.NoError(t, err)
		require.Equal(t, types.Unknown, vatx.Validity())
	})
	require.NoError(t, err)
}

func newActivationTxV1(
	t *testing.T,
	goldenATXID types.ATXID,
	opts ...func(*wire.ActivationTxV1),
) *wire.ActivationTxV1 {
	t.Helper()
	nonce := uint64(999)
	poetRef := types.RandomHash()
	atx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PrevATXID:        types.EmptyATXID,
				PublishEpoch:     postGenesisEpoch,
				PositioningATXID: goldenATXID,
				CommitmentATXID:  &goldenATXID,
				InitialPost:      &wire.PostV1{},
			},
			NIPost:   newNIPosV1tWithPoet(t, poetRef.Bytes()),
			VRFNonce: &nonce,
			Coinbase: types.GenerateAddress([]byte("aaaa")),
			NumUnits: 100,
		},
	}
	for _, opt := range opts {
		opt(atx)
	}
	return atx
}

func newChainedActivationTxV1(
	t *testing.T,
	goldenATXID types.ATXID,
	prev *wire.ActivationTxV1,
	pos types.ATXID,
) *wire.ActivationTxV1 {
	t.Helper()
	poetRef := types.RandomHash()
	return &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PrevATXID:        prev.ID(),
				PublishEpoch:     prev.PublishEpoch + 1,
				PositioningATXID: prev.ID(),
			},
			NIPost:   newNIPosV1tWithPoet(t, poetRef.Bytes()),
			Coinbase: prev.Coinbase,
			NumUnits: prev.NumUnits,
		},
	}
}
