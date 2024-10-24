package activation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"testing/quick"
	"time"

	"github.com/spacemeshos/merkle-tree"
	poetShared "github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

const layersPerEpochBig = 1000

func newMerkleProof(tb testing.TB, leafs []types.Hash32) (types.MerkleProof, types.Hash32) {
	tb.Helper()
	tree, err := merkle.NewTreeBuilder().
		WithHashFunc(poetShared.HashMembershipTreeNode).
		WithLeavesToProve(map[uint64]bool{0: true}).
		Build()
	require.NoError(tb, err)
	for _, m := range leafs {
		require.NoError(tb, tree.AddLeaf(m[:]))
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

func newNIPostWithPoet(tb testing.TB, poetRef []byte) *nipost.NIPostState {
	tb.Helper()
	proof, _ := newMerkleProof(tb, []types.Hash32{
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

func newNIPosV1tWithPoet(tb testing.TB, poetRef []byte) *wire.NIPostV1 {
	tb.Helper()
	proof, _ := newMerkleProof(tb, []types.Hash32{
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

func toAtx(tb testing.TB, watx *wire.ActivationTxV1) *types.ActivationTx {
	tb.Helper()
	atx := wire.ActivationTxFromWireV1(watx)
	atx.SetReceived(time.Now())
	atx.BaseTickHeight = uint64(atx.PublishEpoch)
	atx.TickCount = 1
	return atx
}

type handlerMocks struct {
	goldenATXID types.ATXID

	mclock      *MocklayerClock
	mpub        *pubsubmocks.MockPublisher
	mockFetch   *mocks.MockFetcher
	mValidator  *MocknipostValidator
	mbeacon     *MockAtxReceiver
	mtortoise   *mocks.MockTortoise
	mMalPublish *MockatxMalfeasancePublisher
}

type testHandler struct {
	*Handler

	cdb        *datastore.CachedDB
	edVerifier *signing.EdVerifier

	handlerMocks
}

type atxHandleOpts struct {
	postVerificationDuration time.Duration
	poetLeaves               uint64
	distributedPost          bool
}

func (h *handlerMocks) expectAtxV1(atx *wire.ActivationTxV1, nodeId types.NodeID, opts ...func(*atxHandleOpts)) {
	settings := atxHandleOpts{}
	for _, opt := range opts {
		opt(&settings)
	}
	h.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())

	if atx.VRFNonce != nil {
		h.mValidator.EXPECT().
			VRFNonce(nodeId, h.goldenATXID, *atx.VRFNonce, atx.NIPost.PostMetadata.LabelsPerUnit, atx.NumUnits)
	}
	h.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	h.mockFetch.EXPECT().GetPoetProof(gomock.Any(), types.BytesToHash(atx.NIPost.PostMetadata.Challenge))
	if atx.PrevATXID == types.EmptyATXID {
		h.mValidator.EXPECT().InitialNIPostChallengeV1(gomock.Any(), gomock.Any(), h.goldenATXID)
		h.mValidator.EXPECT().
			Post(gomock.Any(), nodeId, h.goldenATXID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
			DoAndReturn(func(
				_ context.Context, _ types.NodeID, _ types.ATXID, _ *types.Post,
				_ *types.PostMetadata, _ uint32, _ ...validatorOption,
			) error {
				time.Sleep(settings.postVerificationDuration)
				return nil
			})
	} else {
		h.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), nodeId)
	}
	h.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), h.goldenATXID, atx.PublishEpoch)
	h.mValidator.EXPECT().
		NIPost(gomock.Any(), nodeId, h.goldenATXID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
		Return(settings.poetLeaves, nil)
	h.mValidator.EXPECT().IsVerifyingFullPost().Return(!settings.distributedPost)
	h.mbeacon.EXPECT().OnAtx(gomock.Any())
	h.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
}

func newTestHandlerMocks(tb testing.TB, golden types.ATXID) handlerMocks {
	ctrl := gomock.NewController(tb)
	return handlerMocks{
		goldenATXID: golden,
		mclock:      NewMocklayerClock(ctrl),
		mpub:        pubsubmocks.NewMockPublisher(ctrl),
		mockFetch:   mocks.NewMockFetcher(ctrl),
		mValidator:  NewMocknipostValidator(ctrl),
		mbeacon:     NewMockAtxReceiver(ctrl),
		mtortoise:   mocks.NewMockTortoise(ctrl),
		mMalPublish: NewMockatxMalfeasancePublisher(ctrl),
	}
}

func newTestHandler(tb testing.TB, goldenATXID types.ATXID, opts ...HandlerOption) *testHandler {
	lg := zaptest.NewLogger(tb)
	cdb := datastore.NewCachedDB(statesql.InMemoryTest(tb), lg)
	edVerifier := signing.NewEdVerifier()

	mocks := newTestHandlerMocks(tb, goldenATXID)
	atxHdlr := NewHandler(
		"localID",
		cdb,
		atxsdata.New(),
		edVerifier,
		mocks.mclock,
		mocks.mpub,
		mocks.mockFetch,
		goldenATXID,
		mocks.mValidator,
		mocks.mbeacon,
		mocks.mtortoise,
		lg,
		opts...,
	)
	return &testHandler{
		Handler:    atxHdlr,
		cdb:        cdb,
		edVerifier: edVerifier,

		handlerMocks: mocks,
	}
}

func TestHandler_PostMalfeasanceProofs(t *testing.T) {
	t.Run("produced but not published during sync", func(t *testing.T) {
		t.Parallel()
		goldenATXID := types.ATXID{2, 3, 4}
		atxHdlr := newTestHandler(t, goldenATXID)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		malicious, err := identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().VRFNonce(atx.SmesherID, goldenATXID, *atx.VRFNonce, gomock.Any(), atx.NumUnits)
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), *atx.CommitmentATXID, gomock.Any(), gomock.Any(), atx.NumUnits)
		atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().InitialNIPostChallengeV1(gomock.Any(), gomock.Any(), goldenATXID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), atx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
			Return(0, &verifying.ErrInvalidIndex{Index: 2})
		atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())

		msg := codec.MustEncode(atx)
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), types.Hash32{}, p2p.NoPeer, msg))

		// identity is still marked as malicious
		malicious, err = identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("produced and published during gossip", func(t *testing.T) {
		t.Parallel()
		goldenATXID := types.ATXID{2, 3, 4}
		atxHdlr := newTestHandler(t, goldenATXID)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		malicious, err := identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().VRFNonce(atx.SmesherID, goldenATXID, *atx.VRFNonce, gomock.Any(), atx.NumUnits)
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), *atx.CommitmentATXID, gomock.Any(), gomock.Any(), atx.NumUnits)
		atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().InitialNIPostChallengeV1(gomock.Any(), gomock.Any(), goldenATXID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), atx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
			Return(0, &verifying.ErrInvalidIndex{Index: 2})
		atxHdlr.mtortoise.EXPECT().OnMalfeasance(gomock.Any())
		msg := codec.MustEncode(atx)

		postVerifier := NewMockPostVerifier(gomock.NewController(t))
		mh := NewInvalidPostIndexHandler(atxHdlr.cdb, atxHdlr.edVerifier, postVerifier)
		atxHdlr.mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).
			DoAndReturn(func(_ context.Context, _ string, data []byte) error {
				var got mwire.MalfeasanceGossip
				require.NoError(t, codec.Decode(data, &got))
				require.Equal(t, mwire.InvalidPostIndex, got.Proof.Type)
				postVerifier.EXPECT().
					Verify(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(errors.New("invalid"))
				nodeID, err := mh.Validate(context.Background(), got.Proof.Data)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				p, ok := got.Proof.Data.(*mwire.InvalidPostIndexProof)
				require.True(t, ok)
				require.EqualValues(t, 2, p.InvalidIdx)
				return nil
			})
		require.ErrorIs(t, atxHdlr.HandleGossipAtx(context.Background(), p2p.NoPeer, msg), errMaliciousATX)

		malicious, err = identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)
	})
}

func TestHandler_ProcessAtxStoresNewVRFNonce(t *testing.T) {
	goldenATXID := types.RandomATXID()
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1 := newInitialATXv1(t, goldenATXID)
	atx1.Sign(sig)
	atxHdlr.expectAtxV1(atx1, sig.NodeID())
	require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx1)))

	got, err := atxs.VRFNonce(atxHdlr.cdb, sig.NodeID(), atx1.PublishEpoch+1)
	require.NoError(t, err)
	require.Equal(t, types.VRFPostIndex(*atx1.VRFNonce), got)

	atx2 := newChainedActivationTxV1(t, atx1, atx1.ID())
	nonce2 := types.VRFPostIndex(456)
	atx2.VRFNonce = (*uint64)(&nonce2)
	atx2.Sign(sig)
	atxHdlr.expectAtxV1(atx2, sig.NodeID())
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
	require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx2)))

	got, err = atxs.VRFNonce(atxHdlr.cdb, sig.NodeID(), atx2.PublishEpoch+1)
	require.NoError(t, err)
	require.Equal(t, nonce2, got)
}

func TestHandler_HandleGossipAtx(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	first := newInitialATXv1(t, goldenATXID)
	first.Sign(sig)

	second := newChainedActivationTxV1(t, first, first.ID())
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
	require.ErrorIs(t, err, sql.ErrNotFound)

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

	atx1 := newInitialATXv1(t, goldenATXID)
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

func TestHandler_HandleMaliciousAtx(t *testing.T) {
	t.Run("produced but not published during sync", func(t *testing.T) {
		t.Parallel()
		goldenATXID := types.ATXID{2, 3, 4}
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atxHdlr := newTestHandler(t, goldenATXID)
		atx1 := newInitialATXv1(t, goldenATXID)
		atx1.Sign(sig)
		atxHdlr.expectAtxV1(atx1, sig.NodeID())
		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx1)))

		malicious, err := identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)

		atx2 := newInitialATXv1(t, goldenATXID, func(a *wire.ActivationTxV1) {
			a.NumUnits = atx1.NumUnits + 1
		})
		atx2.Sign(sig)
		atxHdlr.expectAtxV1(atx2, sig.NodeID())

		atxHdlr.mtortoise.EXPECT().OnMalfeasance(sig.NodeID())
		msg := codec.MustEncode(atx2)
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), types.Hash32{}, "", msg))

		// identity is still marked as malicious
		malicious, err = identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("produced and published during gossip", func(t *testing.T) {
		t.Parallel()
		goldenATXID := types.ATXID{2, 3, 4}
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atxHdlr := newTestHandler(t, goldenATXID)
		atx1 := newInitialATXv1(t, goldenATXID)
		atx1.Sign(sig)
		atxHdlr.expectAtxV1(atx1, sig.NodeID())
		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx1)))

		malicious, err := identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)

		atx2 := newInitialATXv1(t, goldenATXID, func(a *wire.ActivationTxV1) {
			a.NumUnits = atx1.NumUnits + 1
		})
		atx2.Sign(sig)
		atxHdlr.expectAtxV1(atx2, sig.NodeID())
		atxHdlr.mtortoise.EXPECT().OnMalfeasance(sig.NodeID())
		msg := codec.MustEncode(atx2)

		mh := NewMalfeasanceHandler(atxHdlr.cdb, atxHdlr.logger, atxHdlr.edVerifier)
		atxHdlr.mpub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof, gomock.Any()).
			DoAndReturn(func(_ context.Context, _ string, data []byte) error {
				var got mwire.MalfeasanceGossip
				require.NoError(t, codec.Decode(data, &got))
				require.Equal(t, mwire.MultipleATXs, got.Proof.Type)
				nodeID, err := mh.Validate(context.Background(), got.Proof.Data)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				return nil
			})
		require.ErrorIs(t, atxHdlr.HandleGossipAtx(context.Background(), p2p.NoPeer, msg), errMaliciousATX)

		malicious, err = identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)
	})
}

func TestHandler_HandleSyncedAtx(t *testing.T) {
	goldenATXID := types.RandomATXID()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("missing nipost", func(t *testing.T) {
		t.Parallel()

		atx := newInitialATXv1(t, goldenATXID, func(a *wire.ActivationTxV1) { a.NIPost = nil })
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

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)
		buf := codec.MustEncode(atx)

		atxHdlr := newTestHandler(t, goldenATXID)
		atxHdlr.expectAtxV1(atx, sig.NodeID())

		require.NoError(t, atxHdlr.HandleGossipAtx(context.Background(), "", buf))
		require.ErrorIs(t, atxHdlr.HandleGossipAtx(context.Background(), "", buf), errKnownAtx)
		require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf))
	})

	t.Run("known atx from local id is allowed", func(t *testing.T) {
		t.Parallel()

		atx := newInitialATXv1(t, goldenATXID)
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

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)
		atx.Signature = types.RandomEdSignature()
		buf := codec.MustEncode(atx)

		atxHdlr := newTestHandler(t, goldenATXID)
		err := atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, buf)
		require.ErrorIs(t, err, errMalformedData)
		require.ErrorContains(t, err, "invalid atx signature")
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})
	t.Run("atx V2", func(t *testing.T) {
		t.Parallel()

		atx := newInitialATXv2(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr := newTestHandler(t, goldenATXID, WithAtxVersions(AtxVersions{0: types.AtxV2}))
		atxHdlr.expectInitialAtxV2(atx)
		err := atxHdlr.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), p2p.NoPeer, codec.MustEncode(atx))
		require.NoError(t, err)
	})
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

func TestHandler_AtxWeight(t *testing.T) {
	const (
		peer     = p2p.Peer("buddy")
		tickSize = 3
		units    = 4
		leaves   = uint64(11)
	)

	goldenATXID := types.ATXID{2, 3, 4}
	atxHdlr := newTestHandler(t, goldenATXID, WithTickSize(tickSize))

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx1 := newInitialATXv1(t, goldenATXID)
	atx1.NumUnits = units
	atx1.Sign(sig)
	buf := codec.MustEncode(atx1)

	atxHdlr.expectAtxV1(atx1, sig.NodeID(), func(o *atxHandleOpts) { o.poetLeaves = leaves })
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx1.ID().Hash32(), peer, buf))

	stored1, err := atxHdlr.cdb.GetAtx(atx1.ID())
	require.NoError(t, err)
	require.Equal(t, uint64(0), stored1.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored1.TickCount)
	require.Equal(t, leaves/tickSize, stored1.TickHeight())
	require.Equal(t, (leaves/tickSize)*units, stored1.Weight)

	atx2 := newChainedActivationTxV1(t, atx1, atx1.ID())
	atx2.Sign(sig)
	buf = codec.MustEncode(atx2)

	atxHdlr.expectAtxV1(atx2, sig.NodeID(), func(o *atxHandleOpts) { o.poetLeaves = leaves })
	atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atx1.ID()}, gomock.Any())
	require.NoError(t, atxHdlr.HandleSyncedAtx(context.Background(), atx2.ID().Hash32(), peer, buf))

	stored2, err := atxHdlr.cdb.GetAtx(atx2.ID())
	require.NoError(t, err)
	require.Equal(t, stored1.TickHeight(), stored2.BaseTickHeight)
	require.Equal(t, leaves/tickSize, stored2.TickCount)
	require.Equal(t, stored1.TickHeight()+leaves/tickSize, stored2.TickHeight())
	require.Equal(t, int(leaves/tickSize)*units, int(stored2.Weight))
}

func TestHandler_WrongHash(t *testing.T) {
	goldenATXID := types.RandomATXID()
	atxHdlr := newTestHandler(t, goldenATXID)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx := newInitialATXv1(t, goldenATXID)
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

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr := newTestHandler(t, goldenATXID)
		atxHdlr.expectAtxV1(atx, sig.NodeID(), func(o *atxHandleOpts) { o.distributedPost = false })
		err := atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx))
		require.NoError(t, err)

		vatx, err := atxs.Get(atxHdlr.cdb, atx.ID())
		require.NoError(t, err)
		require.Equal(t, types.Valid, vatx.Validity())
	})
	t.Run("post not verified fully (distributed post)", func(t *testing.T) {
		t.Parallel()

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr := newTestHandler(t, goldenATXID)
		atxHdlr.expectAtxV1(atx, sig.NodeID(), func(o *atxHandleOpts) { o.distributedPost = true })
		err := atxHdlr.HandleGossipAtx(context.Background(), "", codec.MustEncode(atx))
		require.NoError(t, err)

		vatx, err := atxs.Get(atxHdlr.cdb, atx.ID())
		require.NoError(t, err)
		require.Equal(t, types.Unknown, vatx.Validity())
	})
	require.NoError(t, err)
}

func newInitialATXv1(
	t testing.TB,
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
	t testing.TB,
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
				PositioningATXID: pos,
			},
			NIPost:   newNIPosV1tWithPoet(t, poetRef.Bytes()),
			Coinbase: prev.Coinbase,
			NumUnits: prev.NumUnits,
		},
	}
}

func TestHandler_DeterminesAtxVersion(t *testing.T) {
	t.Parallel()

	goldenATXID := types.RandomATXID()

	t.Run("v1", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID)

		atx := newInitialATXv1(t, goldenATXID)
		atx.PublishEpoch = 2

		version, err := atxHdlr.determineVersion(codec.MustEncode(atx))
		require.NoError(t, err)
		require.Equal(t, types.AtxV1, *version)

		atx.PublishEpoch = 10
		version, err = atxHdlr.determineVersion(codec.MustEncode(atx))
		require.NoError(t, err)
		require.Equal(t, types.AtxV1, *version)
	})
	t.Run("v2", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID, WithAtxVersions(map[types.EpochID]types.AtxVersion{10: types.AtxV2}))

		version, err := atxHdlr.determineVersion(codec.MustEncode(types.EpochID(2)))
		require.NoError(t, err)
		require.Equal(t, types.AtxV1, *version)

		version, err = atxHdlr.determineVersion(codec.MustEncode(types.EpochID(10)))
		require.NoError(t, err)
		require.Equal(t, types.AtxV2, *version)

		version, err = atxHdlr.determineVersion(codec.MustEncode(types.EpochID(11)))
		require.NoError(t, err)
		require.Equal(t, types.AtxV2, *version)
	})
	t.Run("v2 since epoch 0", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newTestHandler(t, goldenATXID, WithAtxVersions(map[types.EpochID]types.AtxVersion{0: types.AtxV2}))
		version, err := atxHdlr.determineVersion(codec.MustEncode(types.EpochID(0)))
		require.NoError(t, err)
		require.Equal(t, types.AtxV2, *version)
	})
}

func Test_ValidateAtxVersions(t *testing.T) {
	t.Parallel()
	t.Run("valid 1", func(t *testing.T) {
		t.Parallel()
		versions := AtxVersions(map[types.EpochID]types.AtxVersion{0: types.AtxV1})
		require.NoError(t, versions.Validate())
	})
	t.Run("valid 2", func(t *testing.T) {
		t.Parallel()
		versions := AtxVersions(map[types.EpochID]types.AtxVersion{0: types.AtxV1, 1: types.AtxV2})
		require.NoError(t, versions.Validate())
	})
	t.Run("cannot decrease version", func(t *testing.T) {
		t.Parallel()
		versions := AtxVersions(map[types.EpochID]types.AtxVersion{10: types.AtxV1, 9: types.AtxV2})
		require.Error(t, versions.Validate())
	})
	t.Run("out of range", func(t *testing.T) {
		t.Parallel()

		require.Error(t, AtxVersions(map[types.EpochID]types.AtxVersion{0: types.AtxVMAX + 1}).Validate())
		require.Error(t, AtxVersions(map[types.EpochID]types.AtxVersion{0: 0}).Validate())
	})
}

func TestAtxVersions_SortsVersions(t *testing.T) {
	t.Parallel()

	err := quick.Check(func(v AtxVersions) bool {
		return sort.SliceIsSorted(v.asSlice(), func(i, j int) bool {
			return v.asSlice()[i].publish < v.asSlice()[j].publish
		})
	}, nil)
	require.NoError(t, err)
}

func TestHandler_DecodeATX(t *testing.T) {
	t.Parallel()

	t.Run("malformed publish epoch", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, types.RandomATXID())
		_, err := atxHdlr.decodeATX(nil)
		require.ErrorIs(t, err, errMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})
	t.Run("malformed atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, types.RandomATXID())
		_, err := atxHdlr.decodeATX([]byte("malformed"))
		require.ErrorIs(t, err, errMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})
	t.Run("v1", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newTestHandler(t, types.RandomATXID())

		atx := newInitialATXv1(t, atxHdlr.goldenATXID)
		decoded, err := atxHdlr.decodeATX(atx.Blob().Blob)
		require.NoError(t, err)
		require.Equal(t, atx, decoded)
	})
	t.Run("v2", func(t *testing.T) {
		t.Parallel()
		versions := map[types.EpochID]types.AtxVersion{10: types.AtxV2}
		atxHdlr := newTestHandler(t, types.RandomATXID(), WithAtxVersions(versions))

		atx := newInitialATXv2(t, atxHdlr.goldenATXID)
		atx.PublishEpoch = 10
		decoded, err := atxHdlr.decodeATX(atx.Blob().Blob)
		require.NoError(t, err)
		require.Equal(t, atx, decoded)
	})
	t.Run("rejects v2 in epoch when it's not supported", func(t *testing.T) {
		t.Parallel()
		versions := map[types.EpochID]types.AtxVersion{10: types.AtxV2}
		atxHdlr := newTestHandler(t, types.RandomATXID(), WithAtxVersions(versions))

		atx := newInitialATXv2(t, atxHdlr.goldenATXID)
		atx.PublishEpoch = 9
		_, err := atxHdlr.decodeATX(atx.Blob().Blob)
		require.ErrorIs(t, err, errMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})
}
