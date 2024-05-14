package activation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type v2TestHandler struct {
	*HandlerV2

	handlerMocks
}

func newV2TestHandler(tb testing.TB, golden types.ATXID) *v2TestHandler {
	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	mocks := newTestHandlerMocks(tb, golden)
	return &v2TestHandler{
		HandlerV2: &HandlerV2{
			local:           "localID",
			cdb:             cdb,
			atxsdata:        atxsdata.New(),
			edVerifier:      signing.NewEdVerifier(),
			clock:           mocks.mclock,
			tickSize:        1,
			goldenATXID:     golden,
			nipostValidator: mocks.mValidatorV2,
			log:             lg.Zap(),
			fetcher:         mocks.mockFetch,
			beacon:          mocks.mbeacon,
			tortoise:        mocks.mtortoise,
		},
		handlerMocks: mocks,
	}
}

func (h *handlerMocks) expectFetchDeps(atx *wire.ActivationTxV2) {
	h.mockFetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
	h.mockFetch.EXPECT().GetPoetProof(gomock.Any(), atx.NiPosts[0].Challenge)
	_, atxDeps := (&HandlerV2{goldenATXID: h.goldenATXID}).collectAtxDeps(atx)
	if len(atxDeps) != 0 {
		h.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxDeps, gomock.Any())
	}
}

func (h *handlerMocks) expectInitialAtxV2(atx *wire.ActivationTxV2) {
	h.mclock.EXPECT().CurrentLayer().Return(postGenesisEpoch.FirstLayer())
	h.mValidatorV2.EXPECT().VRFNonceV2(
		atx.SmesherID,
		atx.Initial.CommitmentATX,
		types.VRFPostIndex(*atx.VRFNonce),
		atx.NiPosts[0].Posts[0].NumUnits,
	)
	h.mValidatorV2.EXPECT().PostV2(
		gomock.Any(),
		atx.SmesherID,
		atx.Initial.CommitmentATX,
		&atx.Initial.Post,
		shared.ZeroChallenge,
		atx.NiPosts[0].Posts[0].NumUnits,
		gomock.Any(),
	)

	h.expectFetchDeps(atx)

	// TODO:
	// 2. expect verifying nipost
	// 3. expect storing ATX
}

func TestHandlerV2_SyntacticallyValidate(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	t.Run("rejects invalid signature", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)

		atxHandler := newV2TestHandler(t, golden)
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "signature")
	})
	t.Run("rejects from future", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.PublishEpoch = 100
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer().Return(0)
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "atx publish epoch is too far in the future")
	})
	t.Run("rejects empty positioning ATX", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.PositioningATX = types.EmptyATXID
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "empty positioning atx")
	})
	t.Run("marriages are not supported (yet)", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.Marriages = []wire.MarriageCertificate{{}}
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "marriages are not supported")
	})
	t.Run("reject golden previous ATX", func(t *testing.T) {
		t.Parallel()
		atx := newSoloATXv2(t, 0, golden, golden)
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "previous atx[0] is the golden ATX")
	})
	t.Run("reject empty previous ATX", func(t *testing.T) {
		t.Parallel()
		atx := newSoloATXv2(t, 0, types.EmptyATXID, golden)
		atx.PreviousATXs = append(atx.PreviousATXs, types.EmptyATXID)
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "previous atx[0] is empty")
	})
}

func TestHandlerV2_SyntacticallyValidate_InitialAtx(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		atxHandler.mValidatorV2.EXPECT().VRFNonceV2(
			sig.NodeID(),
			atx.Initial.CommitmentATX,
			types.VRFPostIndex(*atx.VRFNonce),
			atx.NiPosts[0].Posts[0].NumUnits,
		)
		atxHandler.mValidatorV2.EXPECT().PostV2(
			context.Background(),
			sig.NodeID(),
			atx.Initial.CommitmentATX,
			&atx.Initial.Post,
			shared.ZeroChallenge,
			atx.NiPosts[0].Posts[0].NumUnits,
		)
		require.NoError(t, atxHandler.syntacticallyValidate(context.Background(), atx))
	})
	t.Run("rejects previous ATXs", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.PreviousATXs = []types.ATXID{types.RandomATXID()}
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "initial atx must not have previous atxs")

		atx.PreviousATXs = []types.ATXID{types.EmptyATXID}
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		err = atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "initial atx must not have previous atxs")
	})
	t.Run("rejects when VRF nonce is nil", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.VRFNonce = nil
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "initial atx missing vrf nonce")
	})
	t.Run("rejects when Coinbase is nil", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.Coinbase = nil
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "initial atx missing coinbase")
	})
	t.Run("rejects when marriage ATX ref is set", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.MarriageATX = &golden
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "initial atx cannot reference a marriage atx")
	})
	t.Run("rejects when commitment ATX is missing", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.Initial.CommitmentATX = types.EmptyATXID
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "initial atx missing commitment atx")
	})
	t.Run("invalid VRF nonce", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		atxHandler.mValidatorV2.EXPECT().
			VRFNonceV2(
				sig.NodeID(),
				atx.Initial.CommitmentATX,
				types.VRFPostIndex(*atx.VRFNonce),
				atx.NiPosts[0].Posts[0].NumUnits,
			).
			Return(errors.New("invalid nonce"))

		require.ErrorContains(t, atxHandler.syntacticallyValidate(context.Background(), atx), "invalid nonce")
	})
	t.Run("invalid initial PoST", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.Sign(sig)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.mclock.EXPECT().CurrentLayer()
		atxHandler.mValidatorV2.EXPECT().VRFNonceV2(
			sig.NodeID(),
			atx.Initial.CommitmentATX,
			types.VRFPostIndex(*atx.VRFNonce),
			atx.NiPosts[0].Posts[0].NumUnits,
		)
		atxHandler.mValidatorV2.EXPECT().
			PostV2(
				context.Background(),
				sig.NodeID(),
				atx.Initial.CommitmentATX,
				&atx.Initial.Post,
				shared.ZeroChallenge,
				atx.NiPosts[0].Posts[0].NumUnits,
			).
			Return(errors.New("invalid post"))
		require.ErrorContains(t, atxHandler.syntacticallyValidate(context.Background(), atx), "invalid post")
	})
}

func TestHandlerV2_SyntacticallyValidate_SoloAtx(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	atxHandler := newV2TestHandler(t, golden)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid", func(t *testing.T) {
		atx := newSoloATXv2(t, 0, types.RandomATXID(), types.RandomATXID())
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.NoError(t, err)
	})
	t.Run("must have 1 previous ATX", func(t *testing.T) {
		atx := newSoloATXv2(t, 0, types.RandomATXID(), types.RandomATXID())
		atx.PreviousATXs = append(atx.PreviousATXs, types.RandomATXID())
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "solo atx must have one previous atx")
	})
	t.Run("rejects when len(NIPoSTs) != 1", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.NiPosts = append(atx.NiPosts, wire.NiPostsV2{})
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "solo atx must have one nipost")
	})
	t.Run("rejects when contains more than 1 ID", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.NiPosts[0].Posts = append(atx.NiPosts[0].Posts, wire.SubPostV2{})
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "solo atx must have one post")
	})
	t.Run("rejects when PrevATXIndex != 0", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.NiPosts[0].Posts[0].PrevATXIndex = 1
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "solo atx post must have prevATXIndex 0")
	})
}

func TestHandlerV2_SyntacticallyValidate_MergedAtx(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	atxHandler := newV2TestHandler(t, golden)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("merged ATXs are not supported yet", func(t *testing.T) {
		t.Parallel()
		atx := newSoloATXv2(t, 0, types.RandomATXID(), types.RandomATXID())
		atx.MarriageATX = &golden
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		err := atxHandler.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "atx merge is not supported")
	})
}

func TestHandlerV2_ProcessSoloATX(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	peer := peer.ID("other")
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("initial ATX", func(t *testing.T) {
		t.Parallel()
		atx := newInitialATXv2(t, golden)
		atx.Sign(sig)
		blob := codec.MustEncode(atx)

		atxHandler := newV2TestHandler(t, golden)
		atxHandler.expectInitialAtxV2(atx)

		proof, err := atxHandler.processATX(context.Background(), peer, atx, blob, time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		// TODO: verify that the ATX was added to the DB
		// TODO: processing ATX for the second time should skip checks
		// proof, err = atxHandler.processATX(context.Background(), peer, atx, blob, time.Now())
		// require.NoError(t, err)
		// require.Nil(t, proof)
	})
	// TODO: more tests
}

func TestCollectDeps_AtxV2(t *testing.T) {
	goldenATX := types.RandomATXID()
	prev0 := types.RandomATXID()
	prev1 := types.RandomATXID()
	positioning := types.RandomATXID()
	commitment := types.RandomATXID()
	marriage := types.RandomATXID()
	poetA := types.RandomHash()
	poetB := types.RandomHash()

	atxHandler := newV2TestHandler(t, goldenATX)

	t.Run("all unique deps", func(t *testing.T) {
		t.Parallel()
		atx := wire.ActivationTxV2{
			PreviousATXs:   []types.ATXID{prev0, prev1},
			PositioningATX: positioning,
			Initial:        &wire.InitialAtxPartsV2{CommitmentATX: commitment},
			MarriageATX:    &marriage,
			NiPosts: []wire.NiPostsV2{
				{Challenge: poetA},
				{Challenge: poetB},
			},
		}
		poetDeps, atxIDs := atxHandler.collectAtxDeps(&atx)
		require.ElementsMatch(t, []types.Hash32{poetA, poetB}, poetDeps)
		require.ElementsMatch(t, []types.ATXID{prev0, prev1, positioning, commitment, marriage}, atxIDs)
	})
	t.Run("eliminates duplicates", func(t *testing.T) {
		t.Parallel()
		atxA := types.RandomATXID()
		atx := wire.ActivationTxV2{
			PreviousATXs:   []types.ATXID{atxA, atxA},
			PositioningATX: atxA,
			Initial:        &wire.InitialAtxPartsV2{CommitmentATX: atxA},
			MarriageATX:    &atxA,
			NiPosts: []wire.NiPostsV2{
				{Challenge: poetA},
				{Challenge: poetA},
			},
		}
		poetDeps, atxIDs := atxHandler.collectAtxDeps(&atx)
		require.ElementsMatch(t, []types.Hash32{poetA}, poetDeps)
		require.ElementsMatch(t, []types.ATXID{atxA}, atxIDs)
	})
	t.Run("nil commitment ATX", func(t *testing.T) {
		t.Parallel()
		atx := wire.ActivationTxV2{
			PreviousATXs:   []types.ATXID{prev0, prev1},
			PositioningATX: positioning,
			MarriageATX:    &marriage,
			NiPosts: []wire.NiPostsV2{
				{Challenge: poetA},
				{Challenge: poetB},
			},
		}
		poetDeps, atxIDs := atxHandler.collectAtxDeps(&atx)
		require.ElementsMatch(t, []types.Hash32{poetA, poetB}, poetDeps)
		require.ElementsMatch(t, []types.ATXID{prev0, prev1, positioning, marriage}, atxIDs)
	})
	t.Run("filters out golden ATX and empty ATX", func(t *testing.T) {
		t.Parallel()
		atx := wire.ActivationTxV2{
			PreviousATXs:   []types.ATXID{types.EmptyATXID, goldenATX},
			Initial:        &wire.InitialAtxPartsV2{CommitmentATX: goldenATX},
			PositioningATX: goldenATX,
		}
		poetDeps, atxIDs := atxHandler.collectAtxDeps(&atx)
		require.Empty(t, poetDeps)
		require.Empty(t, atxIDs)
	})
}

func TestHandlerV2_RegisterReferences(t *testing.T) {
	atxHdlr := newV2TestHandler(t, types.RandomATXID())

	poets := []types.Hash32{types.RandomHash(), types.RandomHash()}
	atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}
	expectedHashes := poets
	for _, atx := range atxs {
		expectedHashes = append(expectedHashes, atx.Hash32())
	}

	atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(atxHdlr.local, gomock.InAnyOrder(expectedHashes))
	atxHdlr.registerHashes(atxHdlr.local, poets, atxs)
}

func TestHandlerV2_FetchesReferences(t *testing.T) {
	golden := types.RandomATXID()
	t.Run("fetch poet and atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV2TestHandler(t, golden)

		poets := []types.Hash32{types.RandomHash(), types.RandomHash()}
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poets[0])
		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poets[1])
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxs, gomock.Any())
		require.NoError(t, atxHdlr.fetchReferences(context.Background(), poets, atxs))
	})

	t.Run("failed to fetch poet proof", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV2TestHandler(t, golden)

		poets := []types.Hash32{types.RandomHash(), types.RandomHash()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poets[0])
		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poets[1]).Return(errors.New("pooh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poets, nil))
	})

	t.Run("failed to fetch atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV2TestHandler(t, golden)

		poets := []types.Hash32{types.RandomHash(), types.RandomHash()}
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poets[0])
		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poets[1])
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxs, gomock.Any()).Return(errors.New("oh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poets, atxs))
	})
	t.Run("no atxs to fetch", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV2TestHandler(t, golden)

		poets := []types.Hash32{types.RandomHash()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poets[0])
		require.NoError(t, atxHdlr.fetchReferences(context.Background(), poets, nil))
	})
}

func newInitialATXv2(t testing.TB, golden types.ATXID) *wire.ActivationTxV2 {
	t.Helper()
	nonce := uint64(999)
	coinbase := types.GenerateAddress([]byte("aaaa"))
	atx := &wire.ActivationTxV2{
		PositioningATX: golden,
		Initial:        &wire.InitialAtxPartsV2{CommitmentATX: golden},
		NiPosts: []wire.NiPostsV2{
			{
				Challenge: types.RandomHash(),
				Posts: []wire.SubPostV2{
					{
						NumUnits: 4,
					},
				},
			},
		},
		Coinbase: &coinbase,
		VRFNonce: &nonce,
	}

	return atx
}

func newSoloATXv2(t testing.TB, publish types.EpochID, prev, pos types.ATXID) *wire.ActivationTxV2 {
	t.Helper()

	atx := &wire.ActivationTxV2{
		PublishEpoch:   publish,
		PreviousATXs:   []types.ATXID{prev},
		PositioningATX: pos,
		NiPosts: []wire.NiPostsV2{
			{
				Challenge: types.RandomHash(),
				Posts: []wire.SubPostV2{
					{
						NumUnits: 4,
					},
				},
			},
		},
	}

	return atx
}
