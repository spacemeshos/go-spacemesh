package activation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
)

type v1TestHandler struct {
	*HandlerV1

	handlerMocks
}

func newV1TestHandler(tb testing.TB, goldenATXID types.ATXID) *v1TestHandler {
	lg := logtest.New(tb)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	mocks := newTestHandlerMocks(tb, goldenATXID)
	return &v1TestHandler{
		HandlerV1: &HandlerV1{
			local:           "localID",
			cdb:             cdb,
			atxsdata:        atxsdata.New(),
			edVerifier:      signing.NewEdVerifier(),
			clock:           mocks.mclock,
			tickSize:        1,
			goldenATXID:     goldenATXID,
			nipostValidator: mocks.mValidator,
			log:             lg,
			fetcher:         mocks.mockFetch,
			beacon:          mocks.mbeacon,
			tortoise:        mocks.mtortoise,
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
		_, err := atxHdlr.processATX(context.Background(), "", prevAtx, codec.MustEncode(prevAtx), time.Now())
		require.NoError(t, err)

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		posAtx := newInitialATXv1(t, goldenATXID)
		posAtx.Sign(otherSig)
		atxHdlr.expectAtxV1(posAtx, otherSig.NodeID())
		_, err = atxHdlr.processATX(context.Background(), "", posAtx, codec.MustEncode(posAtx), time.Now())
		require.NoError(t, err)
		return atxHdlr, prevAtx, posAtx
	}
	t.Run("valid atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.PositioningATXID = posAtx.ID()
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), atx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(1234, nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), atx.SmesherID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, gomock.Any())
		leaves, units, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
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
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(1234, nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), goldenATXID, newNonce, gomock.Any(), atx.NumUnits)
		leaves, units, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
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
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), goldenATXID, gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1234), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		leaves, units, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
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
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(1234), nil)
		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(gomock.Any(), goldenATXID, *prevAtx.VRFNonce, gomock.Any(), atx.NumUnits)
		leaves, units, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
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
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), gomock.Any())
		atxHdlr.mValidator.EXPECT().
			VRFNonce(gomock.Any(), goldenATXID, *prevAtx.VRFNonce, gomock.Any(), atx.NumUnits).
			Return(errors.New("invalid VRF"))
		_, _, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF")
		require.Nil(t, proof)
	})
	t.Run("valid initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, posAtx := setup(t)

		ctxID := posAtx.ID()
		atx := newInitialATXv1(t, goldenATXID)
		atx.CommitmentATXID = &ctxID
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), gomock.Any(), ctxID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), ctxID, *atx.VRFNonce, gomock.Any(), atx.NumUnits)
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().InitialNIPostChallengeV1(gomock.Any(), gomock.Any(), goldenATXID)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(uint64(777), nil)
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		leaves, units, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
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
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "atx publish epoch is too far in the future")
	})
	t.Run("failing nipost challenge validation", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			NIPostChallengeV1(gomock.Any(), gomock.Any(), atx.SmesherID).
			Return(errors.New("nipost error"))
		_, _, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "nipost error")
		require.Nil(t, proof)
	})
	t.Run("failing positioning atx validation", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), atx.SmesherID)
		atxHdlr.mValidator.EXPECT().
			PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch).
			Return(errors.New("bad positioning atx"))
		_, _, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad positioning atx")
		require.Nil(t, proof)
	})
	t.Run("bad initial nipost challenge", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, posAtx := setup(t)

		cATX := posAtx.ID()
		atx := newInitialATXv1(t, cATX)
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			Post(gomock.Any(), sig.NodeID(), cATX, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any())
		atxHdlr.mValidator.EXPECT().VRFNonce(sig.NodeID(), cATX, *atx.VRFNonce, gomock.Any(), atx.NumUnits)
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().
			InitialNIPostChallengeV1(gomock.Any(), gomock.Any(), goldenATXID).
			Return(errors.New("bad initial nipost"))
		_, _, proof, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "bad initial nipost")
		require.Nil(t, proof)
	})
	t.Run("bad NIPoST", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevATX, postAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevATX, postAtx.ID())
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), atx.SmesherID)
		atxHdlr.mValidator.EXPECT().PositioningAtx(atx.PositioningATXID, gomock.Any(), goldenATXID, atx.PublishEpoch)
		atxHdlr.mValidator.EXPECT().
			NIPost(gomock.Any(), atx.SmesherID, goldenATXID, gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
			Return(0, errors.New("bad nipost"))
		_, _, _, err := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
		require.EqualError(t, err, "invalid nipost: bad nipost")
	})
	t.Run("can't find VRF nonce", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevATX, postAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevATX, postAtx.ID())
		atx.NumUnits += 100
		atx.Sign(sig)

		enc := func(stmt *sql.Statement) { stmt.BindBytes(1, atx.SmesherID.Bytes()) }
		_, err := atxHdlr.cdb.Exec(`UPDATE atxs SET nonce = NULL WHERE pubkey = ?1;`, enc, nil)
		require.NoError(t, err)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		require.NoError(t, atxHdlr.syntacticallyValidate(context.Background(), atx))

		atxHdlr.mValidator.EXPECT().NIPostChallengeV1(gomock.Any(), gomock.Any(), atx.SmesherID)
		_, _, _, err1 := atxHdlr.syntacticallyValidateDeps(context.Background(), atx)
		require.ErrorContains(t, err1, "failed to get current nonce")
	})
	t.Run("missing NodeID in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())
		atx.SmesherID = sig.NodeID()

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "node id is missing")
	})
	t.Run("missing VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.VRFNonce = nil
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "vrf nonce is missing")
	})
	t.Run("invalid VRF nonce in initial atx", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		atxHdlr.mValidator.EXPECT().
			VRFNonce(atx.SmesherID, *atx.CommitmentATXID, *atx.VRFNonce, gomock.Any(), atx.NumUnits).
			Return(errors.New("invalid VRF nonce"))
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.ErrorContains(t, err, "invalid VRF nonce")
	})
	t.Run("prevAtx not declared but initial Post not included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.PrevATXID = types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but initial post is not included")
	})
	t.Run("prevAtx not declared but commitment ATX is not included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.CommitmentATXID = nil
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but commitment atx is missing")
	})
	t.Run("prevAtx not declared but commitment ATX is empty", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.CommitmentATXID = &types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "empty commitment atx")
	})
	t.Run("prevAtx not declared but sequence not zero", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sequence = 1
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "no prev atx declared, but sequence number not zero")
	})
	t.Run("prevAtx not declared but validation of initial post fails", func(t *testing.T) {
		t.Parallel()
		atxHdlr, _, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
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

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "empty positioning atx")
	})
	t.Run("prevAtx declared but initial Post is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, _ := setup(t)

		atx := newInitialATXv1(t, goldenATXID)
		atx.PrevATXID = prevAtx.ID()
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but initial post is included")
	})
	t.Run("prevAtx declared but NodeID is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.NodeID = &types.NodeID{1, 2, 3}
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but node id is included")
	})
	t.Run("prevAtx declared but commitmentATX is included", func(t *testing.T) {
		t.Parallel()
		atxHdlr, prevAtx, posAtx := setup(t)

		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, posAtx.ID())
		atx.CommitmentATXID = &types.EmptyATXID
		atx.Sign(sig)

		atxHdlr.mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		err := atxHdlr.syntacticallyValidate(context.Background(), atx)
		require.EqualError(t, err, "prev atx declared, but commitment atx is included")
	})
}

func TestHandler_ContextuallyValidateAtx(t *testing.T) {
	goldenATXID := types.ATXID{2, 3, 4}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("valid initial atx", func(t *testing.T) {
		t.Parallel()

		atx := newInitialATXv1(t, goldenATXID)
		atx.Sign(sig)

		atxHdlr := newV1TestHandler(t, goldenATXID)
		require.NoError(t, atxHdlr.contextuallyValidateAtx(atx))
	})

	t.Run("missing prevAtx", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newV1TestHandler(t, goldenATXID)

		prevAtx := newInitialATXv1(t, goldenATXID)
		atx := newChainedActivationTxV1(t, goldenATXID, prevAtx, goldenATXID)

		err = atxHdlr.contextuallyValidateAtx(atx)
		require.ErrorIs(t, err, sql.ErrNotFound)
	})

	t.Run("wrong previous atx by same node", func(t *testing.T) {
		t.Parallel()

		atxHdlr := newV1TestHandler(t, goldenATXID)

		atx0 := newInitialATXv1(t, goldenATXID)
		atx0.Sign(sig)
		atxHdlr.expectAtxV1(atx0, sig.NodeID())
		_, err := atxHdlr.processATX(context.Background(), "", atx0, codec.MustEncode(atx0), time.Now())
		require.NoError(t, err)

		atx1 := newChainedActivationTxV1(t, goldenATXID, atx0, goldenATXID)
		atx1.Sign(sig)
		atxHdlr.expectAtxV1(atx1, sig.NodeID())
		atxHdlr.mockFetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any())
		_, err = atxHdlr.processATX(context.Background(), "", atx1, codec.MustEncode(atx1), time.Now())
		require.NoError(t, err)

		atxInvalidPrevious := newChainedActivationTxV1(t, goldenATXID, atx0, goldenATXID)
		atxInvalidPrevious.Sign(sig)
		err = atxHdlr.contextuallyValidateAtx(atxInvalidPrevious)
		require.EqualError(t, err, "last atx is not the one referenced")
	})

	t.Run("wrong previous atx from different node", func(t *testing.T) {
		t.Parallel()

		otherSig, err := signing.NewEdSigner()
		require.NoError(t, err)

		atxHdlr := newV1TestHandler(t, goldenATXID)

		atx0 := newInitialATXv1(t, goldenATXID)
		atx0.Sign(otherSig)
		atxHdlr.expectAtxV1(atx0, otherSig.NodeID())
		_, err = atxHdlr.processATX(context.Background(), "", atx0, codec.MustEncode(atx0), time.Now())
		require.NoError(t, err)

		atx1 := newInitialATXv1(t, goldenATXID)
		atx1.Sign(sig)
		atxHdlr.expectAtxV1(atx1, sig.NodeID())
		_, err = atxHdlr.processATX(context.Background(), "", atx1, codec.MustEncode(atx1), time.Now())
		require.NoError(t, err)

		atxInvalidPrevious := newChainedActivationTxV1(t, goldenATXID, atx0, goldenATXID)
		atxInvalidPrevious.Sign(sig)
		err = atxHdlr.contextuallyValidateAtx(atxInvalidPrevious)
		require.EqualError(t, err, "last atx is not the one referenced")
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
		vAtx := toAtx(t, watx)
		require.NoError(t, err)

		atxHdlr.mbeacon.EXPECT().OnAtx(vAtx)
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), vAtx.ID(), gomock.Any())
		proof, err := atxHdlr.storeAtx(context.Background(), vAtx, watx.Signature)
		require.NoError(t, err)
		require.Nil(t, proof)

		atxFromDb, err := atxs.Get(atxHdlr.cdb, vAtx.ID())
		require.NoError(t, err)
		vAtx.SetReceived(time.Unix(0, vAtx.Received().UnixNano()))
		vAtx.AtxBlob = types.AtxBlob{}
		require.Equal(t, vAtx, atxFromDb)
	})

	t.Run("storing an already known ATX returns no error", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		watx := newInitialATXv1(t, goldenATXID)
		watx.Sign(sig)
		vAtx := toAtx(t, watx)

		atxHdlr.mbeacon.EXPECT().OnAtx(vAtx)
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), vAtx.ID(), gomock.Any())
		proof, err := atxHdlr.storeAtx(context.Background(), vAtx, watx.Signature)
		require.NoError(t, err)
		require.Nil(t, proof)

		atxHdlr.mbeacon.EXPECT().OnAtx(vAtx)
		// Note: tortoise is not informed about the same ATX again
		proof, err = atxHdlr.storeAtx(context.Background(), vAtx, watx.Signature)
		require.NoError(t, err)
		require.Nil(t, proof)
	})

	t.Run("another atx for the same epoch is considered malicious", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)

		watx0 := newInitialATXv1(t, goldenATXID)
		watx0.Sign(sig)
		vAtx0 := toAtx(t, watx0)

		atxHdlr.mbeacon.EXPECT().OnAtx(vAtx0)
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), vAtx0.ID(), gomock.Any())

		proof, err := atxHdlr.storeAtx(context.Background(), vAtx0, watx0.Signature)
		require.NoError(t, err)
		require.Nil(t, proof)

		watx1 := newInitialATXv1(t, goldenATXID)
		watx1.Coinbase = types.GenerateAddress([]byte("aaaa"))
		watx1.Sign(sig)
		vAtx1 := toAtx(t, watx1)

		atxHdlr.mbeacon.EXPECT().OnAtx(vAtx1)
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), vAtx1.ID(), gomock.Any())
		atxHdlr.mtortoise.EXPECT().OnMalfeasance(sig.NodeID())

		proof, err = atxHdlr.storeAtx(context.Background(), vAtx1, watx1.Signature)
		require.NoError(t, err)
		require.NotNil(t, proof)
		require.Equal(t, mwire.MultipleATXs, proof.Proof.Type)

		proof.SetReceived(time.Time{})
		nodeID, err := malfeasance.Validate(
			context.Background(),
			atxHdlr.log,
			atxHdlr.cdb,
			atxHdlr.edVerifier,
			nil,
			&mwire.MalfeasanceGossip{MalfeasanceProof: *proof},
		)
		require.NoError(t, err)
		require.Equal(t, sig.NodeID(), nodeID)

		malicious, err := identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("another atx for the same epoch for registered ID doesn't create a malfeasance proof", func(t *testing.T) {
		atxHdlr := newV1TestHandler(t, goldenATXID)
		atxHdlr.Register(sig)

		watx0 := newInitialATXv1(t, goldenATXID)
		watx0.Sign(sig)
		vAtx0 := toAtx(t, watx0)

		atxHdlr.mbeacon.EXPECT().OnAtx(vAtx0)
		atxHdlr.mtortoise.EXPECT().OnAtx(gomock.Any(), vAtx0.ID(), gomock.Any())

		proof, err := atxHdlr.storeAtx(context.Background(), vAtx0, watx0.Signature)
		require.NoError(t, err)
		require.Nil(t, proof)

		watx1 := newInitialATXv1(t, goldenATXID)
		watx1.Coinbase = types.GenerateAddress([]byte("aaaa"))
		watx1.Sign(sig)
		vAtx1 := toAtx(t, watx1)

		proof, err = atxHdlr.storeAtx(context.Background(), vAtx1, watx1.Signature)
		require.ErrorContains(t,
			err,
			fmt.Sprintf("%s already published an ATX", sig.NodeID().ShortString()),
		)
		require.Nil(t, proof)

		malicious, err := identities.IsMalicious(atxHdlr.cdb, sig.NodeID())
		require.NoError(t, err)
		require.False(t, malicious)
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
}
