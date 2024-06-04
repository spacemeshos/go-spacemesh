package activation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

type v2TestHandler struct {
	*HandlerV2

	handlerMocks
}

func newV2TestHandler(tb testing.TB, golden types.ATXID) *v2TestHandler {
	lg := zaptest.NewLogger(tb)
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
			nipostValidator: mocks.mValidator,
			logger:          lg,
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
		h.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.InAnyOrder(atxDeps), gomock.Any())
	}
}

func (h *handlerMocks) expectVerifyNIPoST(atx *wire.ActivationTxV2) {
	h.mValidator.EXPECT().PostV2(
		gomock.Any(),
		atx.SmesherID,
		gomock.Any(),
		wire.PostFromWireV1(&atx.NiPosts[0].Posts[0].Post),
		atx.NiPosts[0].Challenge.Bytes(),
		atx.NiPosts[0].Posts[0].NumUnits,
		gomock.Any(),
	)
	h.mValidator.EXPECT().PoetMembership(
		gomock.Any(),
		gomock.Any(),
		atx.NiPosts[0].Challenge,
		gomock.Any(),
	)
}

func (h *handlerMocks) expectStoreAtxV2(atx *wire.ActivationTxV2) {
	h.mbeacon.EXPECT().OnAtx(gomock.Any())
	h.mtortoise.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
	h.mValidator.EXPECT().IsVerifyingFullPost().Return(false)
}

func (h *handlerMocks) expectInitialAtxV2(atx *wire.ActivationTxV2) {
	h.mclock.EXPECT().CurrentLayer().Return(postGenesisEpoch.FirstLayer())
	h.mValidator.EXPECT().VRFNonceV2(
		atx.SmesherID,
		atx.Initial.CommitmentATX,
		*atx.VRFNonce,
		atx.NiPosts[0].Posts[0].NumUnits,
	)
	h.mValidator.EXPECT().PostV2(
		gomock.Any(),
		atx.SmesherID,
		atx.Initial.CommitmentATX,
		wire.PostFromWireV1(&atx.Initial.Post),
		shared.ZeroChallenge,
		atx.NiPosts[0].Posts[0].NumUnits,
		gomock.Any(),
	)

	h.expectFetchDeps(atx)
	h.expectVerifyNIPoST(atx)
	h.expectStoreAtxV2(atx)
}

func (h *handlerMocks) expectAtxV2(atx *wire.ActivationTxV2) {
	h.mclock.EXPECT().CurrentLayer().Return(postGenesisEpoch.FirstLayer())
	h.expectFetchDeps(atx)
	h.expectVerifyNIPoST(atx)
	h.expectStoreAtxV2(atx)
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
		atxHandler.mValidator.EXPECT().VRFNonceV2(
			sig.NodeID(),
			atx.Initial.CommitmentATX,
			*atx.VRFNonce,
			atx.NiPosts[0].Posts[0].NumUnits,
		)
		atxHandler.mValidator.EXPECT().PostV2(
			context.Background(),
			sig.NodeID(),
			atx.Initial.CommitmentATX,
			wire.PostFromWireV1(&atx.Initial.Post),
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
		atxHandler.mValidator.EXPECT().
			VRFNonceV2(
				sig.NodeID(),
				atx.Initial.CommitmentATX,
				*atx.VRFNonce,
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
		atxHandler.mValidator.EXPECT().VRFNonceV2(
			sig.NodeID(),
			atx.Initial.CommitmentATX,
			*atx.VRFNonce,
			atx.NiPosts[0].Posts[0].NumUnits,
		)
		atxHandler.mValidator.EXPECT().
			PostV2(
				context.Background(),
				sig.NodeID(),
				atx.Initial.CommitmentATX,
				wire.PostFromWireV1(&atx.Initial.Post),
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

		_, err = atxs.Get(atxHandler.cdb, atx.ID())
		require.NoError(t, err)

		// processing ATX for the second time should skip checks
		proof, err = atxHandler.processATX(context.Background(), peer, atx, blob, time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)
	})
	t.Run("second ATX, previous V1", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)

		prev := newInitialATXv1(t, golden)
		prev.Sign(sig)
		atxs.Add(atxHandler.cdb, toAtx(t, prev))

		atx := newSoloATXv2(t, prev.PublishEpoch+1, prev.ID(), golden)
		atx.Sign(sig)
		blob := codec.MustEncode(atx)
		atxHandler.expectAtxV2(atx)

		proof, err := atxHandler.processATX(context.Background(), peer, atx, blob, time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		atxFromDb, err := atxs.Get(atxHandler.cdb, atx.ID())
		require.NoError(t, err)

		require.Nil(t, atxFromDb.CommitmentATX)
		// copies coinbase and VRF nonce from the previous ATX
		require.Equal(t, prev.Coinbase, atxFromDb.Coinbase)
		require.EqualValues(t, *prev.VRFNonce, atxFromDb.VRFNonce)
	})
	t.Run("second ATX, previous V2", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)

		prev := newInitialATXv2(t, golden)
		prev.Sign(sig)
		blob := codec.MustEncode(prev)

		atxHandler.expectInitialAtxV2(prev)
		proof, err := atxHandler.processATX(context.Background(), peer, prev, blob, time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		atx := newSoloATXv2(t, prev.PublishEpoch+1, prev.ID(), golden)
		atx.Sign(sig)
		blob = codec.MustEncode(atx)
		atxHandler.expectAtxV2(atx)

		proof, err = atxHandler.processATX(context.Background(), peer, atx, blob, time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		_, err = atxs.Get(atxHandler.cdb, atx.ID())
		require.NoError(t, err)
	})
	t.Run("second ATX, previous checkpointed", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)

		prev := atxs.CheckpointAtx{
			ID:            types.RandomATXID(),
			CommitmentATX: types.RandomATXID(),
			NumUnits:      100,
			SmesherID:     sig.NodeID(),
		}
		require.NoError(t, atxs.AddCheckpointed(atxHandler.cdb, &prev))

		atx := newSoloATXv2(t, prev.Epoch+1, prev.ID, golden)
		atx.Sign(sig)
		atxHandler.expectAtxV2(atx)
		_, err := atxHandler.processATX(context.Background(), peer, atx, codec.MustEncode(atx), time.Now())
		require.NoError(t, err)
	})
	t.Run("second ATX, previous V2, increases space (no nonce, previous valid)", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		prev := newInitialATXv2(t, golden)
		prev.Sign(sig)

		atxHandler.expectInitialAtxV2(prev)
		proof, err := atxHandler.processATX(context.Background(), peer, prev, codec.MustEncode(prev), time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		atx := newSoloATXv2(t, prev.PublishEpoch+1, prev.ID(), golden)
		atx.NiPosts[0].Posts[0].NumUnits *= 10
		atx.Sign(sig)
		atxHandler.expectAtxV2(atx)
		atxHandler.mValidator.EXPECT().VRFNonceV2(
			sig.NodeID(),
			prev.Initial.CommitmentATX,
			*prev.VRFNonce,
			atx.TotalNumUnits(),
		)

		proof, err = atxHandler.processATX(context.Background(), peer, atx, codec.MustEncode(atx), time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		// picks the VRF nonce from the previous ATX
		atxFromDb, err := atxs.Get(atxHandler.cdb, atx.ID())
		require.NoError(t, err)
		require.EqualValues(t, *prev.VRFNonce, atxFromDb.VRFNonce)
	})
	t.Run("second ATX, previous V2, increases space (no nonce, previous invalid)", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		prev := newInitialATXv2(t, golden)
		prev.Sign(sig)

		atxHandler.expectInitialAtxV2(prev)
		proof, err := atxHandler.processATX(context.Background(), peer, prev, codec.MustEncode(prev), time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		atx := newSoloATXv2(t, prev.PublishEpoch+1, prev.ID(), golden)
		atx.NiPosts[0].Posts[0].NumUnits *= 10
		atx.Sign(sig)
		atxHandler.mclock.EXPECT().CurrentLayer().Return(postGenesisEpoch.FirstLayer())
		atxHandler.expectFetchDeps(atx)
		atxHandler.expectVerifyNIPoST(atx)
		atxHandler.mValidator.EXPECT().VRFNonceV2(
			sig.NodeID(),
			prev.Initial.CommitmentATX,
			*prev.VRFNonce,
			atx.TotalNumUnits(),
		).Return(errors.New("vrf nonce is not valid"))

		_, err = atxHandler.processATX(context.Background(), peer, atx, codec.MustEncode(atx), time.Now())
		require.ErrorContains(t, err, "vrf nonce is not valid")

		_, err = atxs.Get(atxHandler.cdb, atx.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
	t.Run("second ATX, increases space (new nonce)", func(t *testing.T) {
		t.Parallel()
		lowerNumUnits := uint32(10)
		atxHandler := newV2TestHandler(t, golden)
		prev := &types.ActivationTx{
			NumUnits:      lowerNumUnits,
			SmesherID:     sig.NodeID(),
			CommitmentATX: &golden,
		}
		prev.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(atxHandler.cdb, prev))

		atx := newSoloATXv2(t, prev.PublishEpoch+1, prev.ID(), golden)
		newNonce := uint64(123)
		atx.VRFNonce = &newNonce
		atx.NiPosts[0].Posts[0].NumUnits *= 10
		atx.Sign(sig)
		atxHandler.expectAtxV2(atx)
		atxHandler.mValidator.EXPECT().VRFNonceV2(
			sig.NodeID(),
			*prev.CommitmentATX,
			*atx.VRFNonce,
			atx.TotalNumUnits(),
		)

		proof, err := atxHandler.processATX(context.Background(), peer, atx, codec.MustEncode(atx), time.Now())
		require.NoError(t, err)
		require.Nil(t, proof)

		// verify that the ATX was added to the DB and it has the lower effective num units
		atxFromDb, err := atxs.Get(atxHandler.cdb, atx.ID())
		require.NoError(t, err)
		require.Equal(t, lowerNumUnits, atxFromDb.TotalNumUnits())
		require.EqualValues(t, newNonce, atxFromDb.VRFNonce)
	})
	t.Run("can't find positioning ATX", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		atx := newSoloATXv2(t, 0, types.RandomATXID(), types.RandomATXID())
		atx.Sign(sig)

		atxHandler.mclock.EXPECT().CurrentLayer()
		atxHandler.expectFetchDeps(atx)
		_, err := atxHandler.processATX(context.Background(), peer, atx, codec.MustEncode(atx), time.Now())
		require.ErrorContains(t, err, "validating positioning atx")

		_, err = atxs.Get(atxHandler.cdb, atx.ID())
		require.ErrorIs(t, err, sql.ErrNotFound)
	})
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

func Test_ValidatePositioningAtx(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()

	t.Run("not found", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)
		_, err := atxHandler.validatePositioningAtx(1, golden, types.RandomATXID())
		require.ErrorContains(t, err, "not found")
	})
	t.Run("golden", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		height, err := atxHandler.validatePositioningAtx(0, golden, golden)
		require.NoError(t, err)
		require.Zero(t, height)
	})
	t.Run("non-golden", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)

		positioningAtx := &types.ActivationTx{
			BaseTickHeight: 100,
		}
		positioningAtx.SetID(types.RandomATXID())
		atxs.Add(atxHandler.cdb, positioningAtx)

		height, err := atxHandler.validatePositioningAtx(1, golden, positioningAtx.ID())
		require.NoError(t, err)
		require.Equal(t, positioningAtx.TickHeight(), height)
	})
	t.Run("reject pos ATX from the same epoch", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)

		positioningAtx := &types.ActivationTx{
			PublishEpoch: 1,
		}
		positioningAtx.SetID(types.RandomATXID())
		atxs.Add(atxHandler.cdb, positioningAtx)

		_, err := atxHandler.validatePositioningAtx(1, golden, positioningAtx.ID())
		require.Error(t, err)
	})
	t.Run("reject pos ATX from a future epoch", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)

		positioningAtx := &types.ActivationTx{
			PublishEpoch: 2,
		}
		positioningAtx.SetID(types.RandomATXID())
		atxs.Add(atxHandler.cdb, positioningAtx)

		_, err := atxHandler.validatePositioningAtx(1, golden, positioningAtx.ID())
		require.Error(t, err)
	})
}

func Test_LoadPreviousATX(t *testing.T) {
	t.Parallel()
	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, types.RandomATXID())
		_, err := atxHandler.previous(context.Background(), types.RandomATXID())
		require.ErrorContains(t, err, "not found")
	})
	t.Run("golden not found", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, types.RandomATXID())
		golden := &types.ActivationTx{}
		golden.SetID(types.RandomATXID())
		_, err := atxHandler.previous(context.Background(), golden.ID())
		require.ErrorContains(t, err, "not found")
	})
	t.Run("golden", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, types.RandomATXID())
		golden := &types.ActivationTx{}
		golden.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(atxHandler.cdb, golden))
		atx, err := atxHandler.previous(context.Background(), golden.ID())
		require.NoError(t, err)
		require.Equal(t, golden.ID(), atx.ID())
	})
	t.Run("v1", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, types.RandomATXID())
		prevWire := newInitialATXv1(t, types.RandomATXID())
		prev := toAtx(t, prevWire)
		require.NoError(t, atxs.Add(atxHandler.cdb, prev))
		atx, err := atxHandler.previous(context.Background(), prev.ID())
		require.NoError(t, err)
		require.Equal(t, prev.ID(), atx.ID())
	})
	t.Run("v2", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, types.RandomATXID())
		prevWire := newInitialATXv2(t, types.RandomATXID())
		prev := &types.ActivationTx{
			AtxBlob: types.AtxBlob{
				Blob:    codec.MustEncode(prevWire),
				Version: types.AtxV2,
			},
		}
		prev.SetID(prevWire.ID())
		require.NoError(t, atxs.Add(atxHandler.cdb, prev))
		atx, err := atxHandler.previous(context.Background(), prev.ID())
		require.NoError(t, err)
		require.Equal(t, prev.ID(), atx.ID())
	})
}

func Test_ValidateCommitmentAtx(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()

	t.Run("golden is fine", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		err := atxHandler.validateCommitmentAtx(golden, golden, 0)
		require.NoError(t, err)
	})
	t.Run("valid", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		commitment := &types.ActivationTx{PublishEpoch: 3}
		commitment.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(atxHandler.cdb, commitment))
		err := atxHandler.validateCommitmentAtx(golden, commitment.ID(), 4)
		require.NoError(t, err)
	})
	t.Run("too new", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		commitment := &types.ActivationTx{PublishEpoch: 3}
		commitment.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(atxHandler.cdb, commitment))
		err := atxHandler.validateCommitmentAtx(golden, commitment.ID(), 3)
		require.ErrorContains(t, err, "must be after commitment atx")
		err = atxHandler.validateCommitmentAtx(golden, commitment.ID(), 2)
		require.ErrorContains(t, err, "must be after commitment atx")
	})
	t.Run("not found", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)
		err := atxHandler.validateCommitmentAtx(golden, types.RandomATXID(), 3)
		require.ErrorContains(t, err, "not found")
	})
}

func Test_ValidatePreviousATX(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	atxHandler := newV2TestHandler(t, golden)
	t.Run("out of range", func(t *testing.T) {
		t.Parallel()
		post := &wire.SubPostV2{
			PrevATXIndex: 1,
		}
		_, err := atxHandler.validatePreviousAtx(types.RandomNodeID(), post, nil)
		require.ErrorContains(t, err, "out of bounds")
	})
	t.Run("previous golden, wrong smesher ID", func(t *testing.T) {
		t.Parallel()
		prev := &types.ActivationTx{SmesherID: types.RandomNodeID()}
		_, err := atxHandler.validatePreviousAtx(types.RandomNodeID(), &wire.SubPostV2{}, []opaqueAtx{prev})
		require.ErrorContains(t, err, "prev golden ATX has different owner")
	})
	t.Run("previous V1, wrong smesher ID", func(t *testing.T) {
		t.Parallel()
		prev := newInitialATXv1(t, golden)
		prev.SmesherID = types.RandomNodeID()
		_, err := atxHandler.validatePreviousAtx(types.RandomNodeID(), &wire.SubPostV2{}, []opaqueAtx{prev})
		require.ErrorContains(t, err, "prev ATX V1 has different owner")
	})
	t.Run("previous V2, wrong smesher ID", func(t *testing.T) {
		t.Parallel()
		prev := newInitialATXv2(t, golden)
		prev.SmesherID = types.RandomNodeID()
		_, err := atxHandler.validatePreviousAtx(types.RandomNodeID(), &wire.SubPostV2{}, []opaqueAtx{prev})
		require.ErrorContains(t, err, "previous solo ATX V2 has different owner")
	})
	t.Run("previous golden, valid", func(t *testing.T) {
		t.Parallel()
		id := types.RandomNodeID()
		prev := &types.ActivationTx{
			SmesherID: id,
			NumUnits:  20,
		}
		units, err := atxHandler.validatePreviousAtx(id, &wire.SubPostV2{NumUnits: 100}, []opaqueAtx{prev})
		require.NoError(t, err)
		require.Equal(t, uint32(20), units)

		units, err = atxHandler.validatePreviousAtx(id, &wire.SubPostV2{NumUnits: 10}, []opaqueAtx{prev})
		require.NoError(t, err)
		require.Equal(t, uint32(10), units)
	})
	t.Run("previous V1, valid", func(t *testing.T) {
		t.Parallel()
		id := types.RandomNodeID()
		prev := newInitialATXv1(t, golden)
		prev.SmesherID = id
		prev.NumUnits = 20
		units, err := atxHandler.validatePreviousAtx(id, &wire.SubPostV2{NumUnits: 100}, []opaqueAtx{prev})
		require.NoError(t, err)
		require.Equal(t, uint32(20), units)

		units, err = atxHandler.validatePreviousAtx(id, &wire.SubPostV2{NumUnits: 10}, []opaqueAtx{prev})
		require.NoError(t, err)
		require.Equal(t, uint32(10), units)
	})
	t.Run("previous V2, valid - owner is same ID", func(t *testing.T) {
		t.Parallel()
		id := types.RandomNodeID()
		prev := newInitialATXv2(t, golden)
		prev.SmesherID = id
		prev.NiPosts[0].Posts[0].NumUnits = 20
		units, err := atxHandler.validatePreviousAtx(id, &wire.SubPostV2{NumUnits: 100}, []opaqueAtx{prev})
		require.NoError(t, err)
		require.Equal(t, uint32(20), units)

		units, err = atxHandler.validatePreviousAtx(id, &wire.SubPostV2{NumUnits: 10}, []opaqueAtx{prev})
		require.NoError(t, err)
		require.Equal(t, uint32(10), units)
	})
}

func TestHandlerV2_SyntacticallyValidateDeps(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("invalid commitment ATX in initial ATX", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)

		atx := newInitialATXv2(t, golden)
		atx.Initial.CommitmentATX = types.RandomATXID()
		atx.Sign(sig)

		_, proof, err := atxHandler.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "verifying commitment ATX")
		require.Nil(t, proof)
	})
	t.Run("can't find previous ATX", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)

		atx := newSoloATXv2(t, 0, types.RandomATXID(), golden)
		atx.Sign(sig)

		_, proof, err := atxHandler.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "fetching previous atx: database: not found")
		require.Nil(t, proof)
	})
	t.Run("previous ATX too new", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)

		prev := &types.ActivationTx{}
		prev.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(atxHandler.cdb, prev))

		atx := newSoloATXv2(t, 0, prev.ID(), golden)
		atx.Sign(sig)

		_, proof, err := atxHandler.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "previous atx is too new")
		require.Nil(t, proof)
	})
	t.Run("previous ATX by different smesher", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)

		prev := &types.ActivationTx{
			SmesherID: types.RandomNodeID(),
		}
		prev.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(atxHandler.cdb, prev))

		atx := newSoloATXv2(t, 2, prev.ID(), golden)
		atx.Sign(sig)

		_, proof, err := atxHandler.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "has different owner")
		require.Nil(t, proof)
	})
	t.Run("invalid PoST", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)

		atx := newInitialATXv2(t, golden)
		atx.Sign(sig)

		atxHandler.mValidator.EXPECT().
			PostV2(
				gomock.Any(),
				sig.NodeID(),
				golden,
				wire.PostFromWireV1(&atx.NiPosts[0].Posts[0].Post),
				atx.NiPosts[0].Challenge.Bytes(),
				atx.TotalNumUnits(),
				gomock.Any(),
			).
			Return(errors.New("post failure"))
		_, proof, err := atxHandler.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "post failure")
		require.Nil(t, proof)
	})
	t.Run("invalid PoST index - generates a malfeasance proof", func(t *testing.T) {
		t.Skip("malfeasance proof is not generated yet")
		atxHandler := newV2TestHandler(t, golden)

		atx := newInitialATXv2(t, golden)
		atx.Sign(sig)

		atxHandler.mValidator.EXPECT().
			PostV2(
				gomock.Any(),
				sig.NodeID(),
				golden,
				wire.PostFromWireV1(&atx.NiPosts[0].Posts[0].Post),
				atx.NiPosts[0].Challenge.Bytes(),
				atx.TotalNumUnits(),
				gomock.Any(),
			).
			Return(verifying.ErrInvalidIndex{Index: 7})
		_, proof, err := atxHandler.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "invalid post")
		require.NotNil(t, proof)
	})
	t.Run("invalid PoET membership proof", func(t *testing.T) {
		atxHandler := newV2TestHandler(t, golden)

		atx := newInitialATXv2(t, golden)
		atx.Sign(sig)

		atxHandler.mValidator.EXPECT().PostV2(
			gomock.Any(),
			sig.NodeID(),
			golden,
			wire.PostFromWireV1(&atx.NiPosts[0].Posts[0].Post),
			atx.NiPosts[0].Challenge.Bytes(),
			atx.TotalNumUnits(),
			gomock.Any(),
		)
		atxHandler.mValidator.EXPECT().
			PoetMembership(gomock.Any(), gomock.Any(), atx.NiPosts[0].Challenge, gomock.Any()).
			Return(0, errors.New("poet failure"))
		_, proof, err := atxHandler.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "poet failure")
		require.Nil(t, proof)
	})
}

func Test_Marriages(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	t.Run("invalid marriage signature", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atx := newInitialATXv2(t, golden)
		atx.Marriages = []wire.MarriageCertificate{{
			ID: types.RandomNodeID(),
		}}
		atx.Sign(sig)

		_, err = atxHandler.processATX(context.Background(), "", atx, codec.MustEncode(atx), time.Now())
		require.ErrorContains(t, err, "invalid marriage[0] signature")
	})
	t.Run("valid signature", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atx := newInitialATXv2(t, golden)
		atx.Marriages = []wire.MarriageCertificate{{
			ID:        otherSig.NodeID(),
			Signature: otherSig.Sign(signing.MARRIAGE, sig.NodeID().Bytes()),
		}}
		atx.Sign(sig)

		atxHandler.expectInitialAtxV2(atx)
		_, err = atxHandler.processATX(context.Background(), "", atx, codec.MustEncode(atx), time.Now())
		require.NoError(t, err)

		married, err := identities.Married(atxHandler.cdb, sig.NodeID())
		require.NoError(t, err)
		require.True(t, married)

		married, err = identities.Married(atxHandler.cdb, otherSig.NodeID())
		require.NoError(t, err)
		require.True(t, married)

		set, err := identities.EquivocationSet(atxHandler.cdb, sig.NodeID())
		require.NoError(t, err)
		require.ElementsMatch(t, []types.NodeID{sig.NodeID(), otherSig.NodeID()}, set)
	})
	t.Run("can't marry twice", func(t *testing.T) {
		t.Parallel()
		atxHandler := newV2TestHandler(t, golden)
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atx := newInitialATXv2(t, golden)
		atx.Marriages = []wire.MarriageCertificate{{
			ID:        otherSig.NodeID(),
			Signature: otherSig.Sign(signing.MARRIAGE, sig.NodeID().Bytes()),
		}}
		atx.Sign(sig)

		atxHandler.expectInitialAtxV2(atx)
		_, err = atxHandler.processATX(context.Background(), "", atx, codec.MustEncode(atx), time.Now())
		require.NoError(t, err)

		// otherSig2 cannot marry sig, trying to extend its set.
		otherSig2, err := signing.NewEdSigner()
		require.NoError(t, err)
		atx2 := newSoloATXv2(t, atx.PublishEpoch+1, atx.ID(), atx.ID())
		atx2.Marriages = []wire.MarriageCertificate{{
			ID:        otherSig2.NodeID(),
			Signature: otherSig2.Sign(signing.MARRIAGE, sig.NodeID().Bytes()),
		}}
		atx2.Sign(sig)
		atxHandler.expectAtxV2(atx2)
		ids := []types.NodeID{sig.NodeID(), otherSig.NodeID(), otherSig2.NodeID()}
		for _, id := range ids {
			atxHandler.mtortoise.EXPECT().OnMalfeasance(id)
		}
		proof, err := atxHandler.processATX(context.Background(), "", atx2, codec.MustEncode(atx2), time.Now())
		require.NoError(t, err)
		// TODO: check the proof contents once its implemented
		require.NotNil(t, proof)

		// All 3 IDs are marked as malicious
		for _, id := range ids {
			malicious, err := identities.IsMalicious(atxHandler.cdb, id)
			require.NoError(t, err)
			require.True(t, malicious)
		}

		// The equivocation set of sig and otherSig didn't grow
		equiv, err := identities.EquivocationSet(atxHandler.cdb, sig.NodeID())
		require.NoError(t, err)
		require.ElementsMatch(t, []types.NodeID{sig.NodeID(), otherSig.NodeID()}, equiv)
	})
}

func Test_MarryingMalicious(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	otherSig, err := signing.NewEdSigner()
	require.NoError(t, err)

	test := func(t *testing.T, malicious types.NodeID) {
		atxHandler := newV2TestHandler(t, golden)

		atx := newInitialATXv2(t, golden)
		atx.Marriages = []wire.MarriageCertificate{{
			ID:        otherSig.NodeID(),
			Signature: otherSig.Sign(signing.MARRIAGE, sig.NodeID().Bytes()),
		}}
		atx.Sign(sig)

		require.NoError(t, identities.SetMalicious(atxHandler.cdb, malicious, []byte("proof"), time.Now()))

		atxHandler.expectInitialAtxV2(atx)
		atxHandler.mtortoise.EXPECT().OnMalfeasance(sig.NodeID())
		atxHandler.mtortoise.EXPECT().OnMalfeasance(otherSig.NodeID())

		_, err = atxHandler.processATX(context.Background(), "", atx, codec.MustEncode(atx), time.Now())
		require.NoError(t, err)

		equiv, err := identities.EquivocationSet(atxHandler.cdb, sig.NodeID())
		require.NoError(t, err)
		require.ElementsMatch(t, []types.NodeID{sig.NodeID(), otherSig.NodeID()}, equiv)

		for _, id := range []types.NodeID{sig.NodeID(), otherSig.NodeID()} {
			m, err := identities.IsMalicious(atxHandler.cdb, id)
			require.NoError(t, err)
			require.True(t, m)
		}
	}
	t.Run("owner is malicious", func(t *testing.T) {
		t.Parallel()
		test(t, sig.NodeID())
	})

	t.Run("other is malicious", func(t *testing.T) {
		t.Parallel()
		test(t, otherSig.NodeID())
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
