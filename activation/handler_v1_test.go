package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type v1TestHandler struct {
	*HandlerV1

	handlerMocks
}

func newV1TestHandler(tb testing.TB, goldenATXID types.ATXID) *v1TestHandler {
	lg := zaptest.NewLogger(tb)
	cdb := datastore.NewCachedDB(statesql.InMemoryTest(tb), lg)
	tb.Cleanup(func() { assert.NoError(tb, cdb.Close()) })
	mocks := newTestHandlerMocks(tb, goldenATXID)
	return &v1TestHandler{
		HandlerV1: &HandlerV1{
			local:           "localID",
			cdb:             cdb,
			atxsdata:        atxsdata.New(),
			edVerifier:      signing.NewEdVerifier(),
			clock:           mocks.mClock,
			tickSize:        1,
			goldenATXID:     goldenATXID,
			nipostValidator: mocks.mValidator,
			logger:          lg,
			fetcher:         mocks.mockFetch,
			beacon:          mocks.mBeacon,
			tortoise:        mocks.mTortoise,
			malPublisher:    mocks.mLegacyMalPublish,
			signers:         make(map[types.NodeID]*signing.EdSigner),
		},
		handlerMocks: mocks,
	}
}

func TestHandlerV1_SyntacticallyValidateAtx(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	goldenATXID := types.RandomATXID()

	setup := func(t *testing.T) (hdlr *v1TestHandler, prev, pos *wire.ActivationTxV1) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		prevAtx := newInitialATXv1(t, goldenATXID)
		prevAtx.NumUnits = 100
		prevAtx.Sign(sig)
		atxHdlr.expectAtxV1(prevAtx, sig.NodeID())
		require.NoError(t, atxHdlr.processATX(context.Background(), p2p.NoPeer, prevAtx, time.Now()))

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		posAtx := newInitialATXv1(t, goldenATXID)
		posAtx.Sign(otherSig)
		atxHdlr.expectAtxV1(posAtx, otherSig.NodeID())
		require.NoError(t, atxHdlr.processATX(context.Background(), p2p.NoPeer, posAtx, time.Now()))
		return atxHdlr, prevAtx, posAtx
	}

	t.Run("valid atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		watx.PositioningATXID = posAtx.ID()
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), watx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(1234, nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), watx.SmesherID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, gomock.Any())
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost().Return(true)
		received := time.Now()
		atx, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.NoError(t, err)
		require.Equal(t, types.Valid, atx.Validity())
		require.Equal(t, received, atx.Received())
		require.EqualValues(t, atx.VRFNonce, *prevAtx.VRFNonce)
		require.Equal(t, watx.NumUnits, atx.NumUnits)
		require.Equal(t, uint64(1234)/atxHdlr.tickSize, atx.TickCount)
		require.Equal(t, uint64(atx.NumUnits)*atx.TickCount, atx.Weight)
	})

	t.Run("valid atx with new VRF nonce", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		newNonce := *prevAtx.VRFNonce + 100
		watx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		watx.VRFNonce = &newNonce
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(1234, nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, watx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), goldenATXID, newNonce, gomock.Any(), watx.NumUnits)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost().Return(true)
		received := time.Now()
		atx, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.NoError(t, err)
		require.Equal(t, types.Valid, atx.Validity())
		require.Equal(t, received, atx.Received())
		require.EqualValues(t, atx.VRFNonce, newNonce)
		require.Equal(t, watx.NumUnits, atx.NumUnits)
		require.Equal(t, uint64(1234)/atxHdlr.tickSize, atx.TickCount)
		require.Equal(t, uint64(atx.NumUnits)*atx.TickCount, atx.Weight)
	})

	t.Run("valid atx with decreasing num units", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		watx.NumUnits = prevAtx.NumUnits - 10
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1234), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, watx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost().Return(true)
		received := time.Now()
		atx, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.NoError(t, err)
		require.Equal(t, types.Valid, atx.Validity())
		require.Equal(t, received, atx.Received())
		require.EqualValues(t, atx.VRFNonce, *prevAtx.VRFNonce)
		require.Equal(t, watx.NumUnits, atx.NumUnits)
		require.Equal(t, uint64(1234)/atxHdlr.tickSize, atx.TickCount)
		require.Equal(t, uint64(atx.NumUnits)*atx.TickCount, atx.Weight)
	})

	t.Run("atx with increasing num units, no new VRF, old valid", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		watx.NumUnits = prevAtx.NumUnits + 10
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1234), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), goldenATXID, *prevAtx.VRFNonce, gomock.Any(), watx.NumUnits)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost().Return(true)
		received := time.Now()
		atx, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.NoError(t, err)
		require.Equal(t, types.Valid, atx.Validity())
		require.Equal(t, received, atx.Received())
		require.EqualValues(t, atx.VRFNonce, *prevAtx.VRFNonce)
		require.Equal(t, prevAtx.NumUnits, atx.NumUnits)
		require.Equal(t, uint64(1234)/atxHdlr.tickSize, atx.TickCount)
		require.Equal(t, uint64(atx.NumUnits)*atx.TickCount, atx.Weight)
	})

	t.Run("atx with increasing num units, no new VRF, old invalid for new size", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		watx.NumUnits = prevAtx.NumUnits + 10
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), goldenATXID, *prevAtx.VRFNonce, gomock.Any(), watx.NumUnits).
			Return(errors.New("invalid VRF"))
		received := time.Now()
		_, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.ErrorContains(t, err, "invalid VRF")
	})

	t.Run("valid initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, posAtx := setup(t)

		ctxID := posAtx.ID()
		watx := newInitialATXv1(t, goldenATXID)
		watx.CommitmentATXID = &ctxID
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), ctxID, gomock.Any(), gomock.Any(), watx.NumUnits, gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), ctxID, *watx.VRFNonce, gomock.Any(), watx.NumUnits)
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().InitialNIPostChallengeV1(gomock.Any(), gomock.Any(), goldenATXID)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(777), nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, watx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().IsVerifyingFullPost().Return(true)
		received := time.Now()
		atx, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.NoError(t, err)
		require.Equal(t, types.Valid, atx.Validity())
		require.Equal(t, received, atx.Received())
		require.EqualValues(t, atx.VRFNonce, *watx.VRFNonce)
		require.Equal(t, watx.NumUnits, atx.NumUnits)
		require.Equal(t, uint64(777)/atxHdlr.tickSize, atx.TickCount)
		require.Equal(t, uint64(atx.NumUnits)*atx.TickCount, atx.Weight)
	})

	t.Run("atx targeting wrong publish epoch", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return((atx.PublishEpoch - 2).FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "atx publish epoch is too far in the future")
	})

	t.Run("failing nipost challenge validation", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().
			NIPostChallengeV1(gomock.Any(), gomock.Any(), watx.SmesherID).
			Return(errors.New("nipost error"))
		received := time.Now()
		_, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.EqualError(t, err, "nipost error")
	})

	t.Run("failing positioning atx validation", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), watx.SmesherID)
		atxHdlr.mValidator.EXPECT().
			PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, watx.PublishEpoch).
			Return(errors.New("bad positioning atx"))
		received := time.Now()
		_, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.EqualError(t, err, "bad positioning atx")
	})

	t.Run("bad initial nipost challenge", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, posAtx := setup(t)

		cATX := posAtx.ID()
		watx := newInitialATXv1(t, cATX)
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), sig.NodeID(), cATX, gomock.Any(), gomock.Any(), watx.NumUnits, gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), cATX, *watx.VRFNonce, gomock.Any(), watx.NumUnits)
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().
			InitialNIPostChallengeV1(gomock.Any(), gomock.Any(), goldenATXID).
			Return(errors.New("bad initial nipost"))
		received := time.Now()
		_, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.EqualError(t, err, "bad initial nipost")
	})

	t.Run("bad NIPoST", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevATX, postAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevATX, postAtx.ID())
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), watx.SmesherID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, watx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), watx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), watx.NumUnits, gomock.Any()).
			Return(0, errors.New("bad nipost"))
		received := time.Now()
		_, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.EqualError(t, err, "validating nipost: bad nipost")
	})

	t.Run("invalid NIPoST", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevATX, postAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevATX, postAtx.ID())
		watx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), watx.SmesherID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, watx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), watx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), watx.NumUnits, gomock.Any()).
			Return(0, &verifying.ErrInvalidIndex{Index: 2})

		atxHdlr.mLegacyMalPublish.EXPECT().PublishProof(context.Background(), watx.SmesherID, gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, mp *mwire.MalfeasanceProof) error {
				require.Equal(t, mwire.InvalidPostIndex, mp.Proof.Type)

				postVerifier := NewMockPostVerifier(atxHdlr.ctrl)
				postVerifier.EXPECT().
					Verify(context.Background(), (*shared.Proof)(watx.NIPost.Post), gomock.Any(), gomock.Any()).
					Return(&verifying.ErrInvalidIndex{Index: 2})

				mh := NewInvalidPostIndexHandler(atxHdlr.cdb, atxHdlr.edVerifier, postVerifier)
				nodeID, err := mh.Validate(context.Background(), mp.Proof.Data)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				return nil
			},
		)

		received := time.Now()
		_, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.ErrorIs(t, err, errMaliciousATX)
	})

	t.Run("invalid NIPoST of known malfeasant", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevATX, postAtx := setup(t)

		watx := newChainedActivationTxV1(t, prevATX, postAtx.ID())
		watx.Sign(sig)

		require.NoError(t, identities.SetMalicious(atxHdlr.cdb, watx.SmesherID, []byte("proof"), time.Now()))

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(watx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), watx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), watx.SmesherID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(watx.PositioningATXID, gomock.Any(), goldenATXID, watx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), watx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), watx.NumUnits, gomock.Any()).
			Return(0, &verifying.ErrInvalidIndex{Index: 2})

		received := time.Now()
		_, err := atxHdlr.syntacticallyValidateDeps(context.Background(), watx, received)
		require.EqualError(t, err, fmt.Sprintf("smesher %s is known malfeasant", watx.SmesherID.ShortString()))
	})

	t.Run("missing NodeID in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())
		atx.SmesherID = sig.NodeID()

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "node id is missing")
	})

	t.Run("missing VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.VRFNonce = nil
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "vrf nonce is missing")
	})

	t.Run("invalid VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			VRFNonce(atx.SmesherID, *atx.CommitmentATXID, *atx.VRFNonce, gomock.Any(), atx.NumUnits).
			Return(errors.New("invalid VRF nonce"))
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF nonce")
	})

	t.Run("prevAtx not declared but initial Post not included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		atx.PrevATXID = types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but initial post is not included")
	})

	t.Run("prevAtx not declared but commitment ATX is not included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.CommitmentATXID = nil
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but commitment atx is missing")
	})

	t.Run("prevAtx not declared but commitment ATX is empty", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.CommitmentATXID = &types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "empty commitment atx")
	})

	t.Run("prevAtx not declared but sequence not zero", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sequence = 1
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but sequence number not zero")
	})

	t.Run("prevAtx not declared but validation of initial post fails", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			VRFNonce(atx.SmesherID, *atx.CommitmentATXID, *atx.VRFNonce, gomock.Any(), atx.NumUnits)
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), atx.SmesherID, gomock.Any(), gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
			Return(errors.New("failed post validation"))
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "failed post validation")
	})

	t.Run("empty positioning ATX", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.PositioningATXID = types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "empty positioning atx")
	})

	t.Run("prevAtx declared but initial Post is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.PrevATXID = prevAtx.ID()
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but initial post is included")
	})

	t.Run("prevAtx declared but NodeID is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		atx.NodeID = &types.NodeID{1, 2, 3}
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but node id is included")
	})

	t.Run("prevAtx declared but commitmentATX is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, prevAtx, posAtx.ID())
		atx.CommitmentATXID = &types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mClock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but commitment atx is included")
	})
}

func TestHandlerV1_StoreAtx(t *testing.T) {
	goldenATXID := types.RandomATXID()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("stores ATX in DB", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		watx := newInitialATXv1(t, goldenATXID)
		watx.Sign(sig)
		atx := toAtx(t, watx)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx.PublishEpoch+1, watx.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx, watx))

		atxFromDb, err := atxs.Get(atxHdlr.cdb, atx.ID())
		require.NoError(t, err)
		atx.SetReceived(time.Unix(0, atx.Received().UnixNano()))
		require.Equal(t, atx, atxFromDb)
	})

	t.Run("storing an already known ATX returns no error", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		watx := newInitialATXv1(t, goldenATXID)
		watx.Sign(sig)
		atx := toAtx(t, watx)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx.PublishEpoch+1, watx.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx, watx))

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx.ID()
		}))
		// Note: tortoise is not informed about the same ATX again
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx, watx))
	})

	t.Run("stores ATX of malicious identity", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		require.NoError(t, identities.SetMalicious(atxHdlr.cdb, sig.NodeID(), types.RandomBytes(10), time.Now()))

		watx := newInitialATXv1(t, goldenATXID)
		watx.Sign(sig)
		atx := toAtx(t, watx)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx.PublishEpoch+1, watx.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx, watx))

		atxFromDb, err := atxs.Get(atxHdlr.cdb, atx.ID())
		require.NoError(t, err)
		atx.SetReceived(time.Unix(0, atx.Received().UnixNano()))
		require.Equal(t, atx, atxFromDb)
	})

	t.Run("another atx for the same epoch is considered malicious", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		watx0 := newInitialATXv1(t, goldenATXID)
		watx0.Sign(sig)
		atx0 := toAtx(t, watx0)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx0.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx0.PublishEpoch+1, watx0.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx0, watx0))

		watx1 := newInitialATXv1(t, goldenATXID)
		watx1.Coinbase = types.GenerateAddress([]byte("aaaa"))
		watx1.Sign(sig)
		atx1 := toAtx(t, watx1)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx1.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx1.PublishEpoch+1, watx1.ID(), gomock.Any())

		atxHdlr.mLegacyMalPublish.EXPECT().PublishProof(context.Background(), sig.NodeID(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, mp *mwire.MalfeasanceProof) error {
				require.Equal(t, mwire.MultipleATXs, mp.Proof.Type)

				mh := NewMalfeasanceHandler(atxHdlr.cdb, atxHdlr.logger, atxHdlr.edVerifier)
				nodeID, err := mh.Validate(context.Background(), mp.Proof.Data)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				return nil
			},
		)
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx1, watx1))
	})

	t.Run("another atx for the same epoch for registered ID doesn't create a malfeasance proof", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)
		atxHdlr.Register(sig)

		watx0 := newInitialATXv1(t, goldenATXID)
		watx0.Sign(sig)
		atx0 := toAtx(t, watx0)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx0.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx0.PublishEpoch+1, watx0.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx0, watx0))

		watx1 := newInitialATXv1(t, goldenATXID)
		watx1.Coinbase = types.GenerateAddress([]byte("aaaa"))
		watx1.Sign(sig)
		atx1 := toAtx(t, watx1)

		require.ErrorContains(t,
			atxHdlr.storeAtx(context.Background(), atx1, watx1),
			fmt.Sprintf("%s already published an ATX", sig.NodeID().ShortString()),
		)
	})

	t.Run("another atx with the same prevatx is considered malicious", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		initialATX := newInitialATXv1(t, goldenATXID)
		initialATX.Sign(sig)
		wInitialATX := toAtx(t, initialATX)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == initialATX.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(initialATX.PublishEpoch+1, initialATX.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), wInitialATX, initialATX))

		// valid first non-initial ATX
		watx1 := newChainedActivationTxV1(t, initialATX, goldenATXID)
		watx1.Sign(sig)
		atx1 := toAtx(t, watx1)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx1.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx1.PublishEpoch+1, watx1.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx1, watx1))

		watx2 := newChainedActivationTxV1(t, watx1, goldenATXID)
		watx2.Sign(sig)
		atx2 := toAtx(t, watx2)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx2.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx2.PublishEpoch+1, watx2.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx2, watx2))

		// third non-initial ATX references initial ATX as prevATX
		watx3 := newChainedActivationTxV1(t, initialATX, goldenATXID)
		watx3.PublishEpoch = watx2.PublishEpoch + 1
		watx3.Sign(sig)
		atx3 := toAtx(t, watx3)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx3.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx3.PublishEpoch+1, watx3.ID(), gomock.Any())

		atxHdlr.mLegacyMalPublish.EXPECT().PublishProof(context.Background(), sig.NodeID(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ types.NodeID, mp *mwire.MalfeasanceProof) error {
				require.Equal(t, mwire.InvalidPrevATX, mp.Proof.Type)

				mh := NewInvalidPrevATXHandler(atxHdlr.cdb, atxHdlr.edVerifier)
				nodeID, err := mh.Validate(context.Background(), mp.Proof.Data)
				require.NoError(t, err)
				require.Equal(t, sig.NodeID(), nodeID)
				return nil
			},
		)

		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx3, watx3))
	})

	t.Run("another atx with the same prevatx for registered ID doesn't create a malfeasance proof", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)
		atxHdlr.Register(sig)

		// Act & Assert
		wInitialATX := newInitialATXv1(t, goldenATXID)
		wInitialATX.Sign(sig)
		initialAtx := toAtx(t, wInitialATX)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == wInitialATX.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(wInitialATX.PublishEpoch+1, wInitialATX.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), initialAtx, wInitialATX))

		// valid first non-initial ATX
		watx1 := newChainedActivationTxV1(t, wInitialATX, goldenATXID)
		watx1.Sign(sig)
		atx1 := toAtx(t, watx1)

		atxHdlr.mBeacon.EXPECT().OnAtx(gomock.Cond(func(atx *types.ActivationTx) bool {
			return atx.ID() == watx1.ID()
		}))
		atxHdlr.mTortoise.EXPECT().OnAtx(watx1.PublishEpoch+1, watx1.ID(), gomock.Any())
		require.NoError(t, atxHdlr.storeAtx(context.Background(), atx1, watx1))

		// second non-initial ATX references empty as prevATX
		watx2 := newInitialATXv1(t, goldenATXID)
		watx2.PublishEpoch = watx1.PublishEpoch + 1
		watx2.Sign(sig)
		atx2 := toAtx(t, watx2)

		require.ErrorContains(t,
			atxHdlr.storeAtx(context.Background(), atx2, watx2),
			fmt.Sprintf("%s referenced incorrect previous ATX", sig.NodeID().ShortString()),
		)
	})
}

func TestHandlerV1_RegistersHashesInPeer(t *testing.T) {
	goldenATXID := types.RandomATXID()
	peer := p2p.Peer("buddy")

	t.Run("registers poet and atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV1TestHandler(t, goldenATXID)

		poet := types.RandomHash()
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().
			RegisterPeerHashes(peer, gomock.InAnyOrder([]types.Hash32{poet, atxs[0].Hash32(), atxs[1].Hash32()}))
		atxHdlr.registerHashes(peer, poet, atxs)
	})

	t.Run("registers poet", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV1TestHandler(t, goldenATXID)

		poet := types.RandomHash()

		atxHdlr.mockFetch.EXPECT().RegisterPeerHashes(peer, []types.Hash32{poet})
		atxHdlr.registerHashes(peer, poet, nil)
	})
}

func TestHandlerV1_FetchesReferences(t *testing.T) {
	goldenATXID := types.RandomATXID()

	t.Run("fetch poet and atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV1TestHandler(t, goldenATXID)

		poet := types.RandomHash()
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet)
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxs, gomock.Any())
		require.NoError(t, atxHdlr.fetchReferences(context.Background(), poet, atxs))
	})

	t.Run("no poet proofs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV1TestHandler(t, goldenATXID)

		poet := types.RandomHash()

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet).Return(errors.New("pooh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poet, nil))
	})

	t.Run("no atxs", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV1TestHandler(t, goldenATXID)

		poet := types.RandomHash()
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet).Return(errors.New("pooh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poet, nil))

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet)
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxs, gomock.Any()).Return(errors.New("oh"))
		require.Error(t, atxHdlr.fetchReferences(context.Background(), poet, atxs))
	})

	t.Run("reject ATX when dependency ATX is rejected", func(t *testing.T) {
		t.Parallel()
		atxHdlr := newV1TestHandler(t, goldenATXID)

		poet := types.RandomHash()
		atxs := []types.ATXID{types.RandomATXID(), types.RandomATXID()}
		var batchErr fetch.BatchError
		batchErr.Add(atxs[0].Hash32(), pubsub.ErrValidationReject)

		atxHdlr.mockFetch.EXPECT().GetPoetProof(gomock.Any(), poet)
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), atxs, gomock.Any()).Return(&batchErr)

		require.ErrorIs(t, atxHdlr.fetchReferences(context.Background(), poet, atxs), pubsub.ErrValidationReject)
	})
}
