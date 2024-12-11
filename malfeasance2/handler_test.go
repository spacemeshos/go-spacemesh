package malfeasance2_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance2"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testHandler struct {
	*malfeasance2.Handler

	observedLogs *observer.ObservedLogs
	db           sql.StateDatabase
	self         p2p.Peer
	mockTrt      *malfeasance2.Mocktortoise
}

func newTestHandler(tb testing.TB) *testHandler {
	db := statesql.InMemory()
	edVerifier := signing.NewEdVerifier()

	observer, observedLogs := observer.New(zap.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	ctrl := gomock.NewController(tb)
	mockTrt := malfeasance2.NewMocktortoise(ctrl)

	h := malfeasance2.NewHandler(
		db,
		logger,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		edVerifier,
		mockTrt,
	)
	return &testHandler{
		Handler: h,

		observedLogs: observedLogs,
		db:           db,
		self:         "self",
		mockTrt:      mockTrt,
	}
}

// TODO(mafa): missing tests
// - new proof for same identity is no-op
// - new proof with bigger certificate list only updates certificate list
// - all identities in certificates are marked as malicious
// - invalid certificates are ignored if proof is valid

func TestHandler_HandleSync(t *testing.T) {
	t.Run("malformed data", func(t *testing.T) {
		h := newTestHandler(t)

		err := h.HandleSynced(context.Background(), types.EmptyHash32, "peer", []byte("malformed"))
		require.ErrorIs(t, err, malfeasance2.ErrMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("unknown version", func(t *testing.T) {
		h := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 42,
		}

		err := h.HandleSynced(context.Background(), types.EmptyHash32, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownVersion)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("unknown domain", func(t *testing.T) {
		h := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  42,
		}

		err := h.HandleSynced(context.Background(), types.EmptyHash32, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownDomain)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("invalid proof", func(t *testing.T) {
		h := newTestHandler(t)
		invalidProof := []byte("invalid")
		handlerError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), invalidProof).Return(types.EmptyNodeID, handlerError)
		mockHandler.EXPECT().ReportLabel().Return("invalidPost")
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   invalidProof,
		}

		err := h.HandleSynced(context.Background(), types.EmptyHash32, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, handlerError)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("valid proof", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabel().Return("invalidPost")
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		h.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := h.HandleSynced(context.Background(), types.Hash32(nodeID), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		malicious, err := malfeasance.IsMalicious(h.db, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("valid proof, wrong hash", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabel().Return("invalidPost")
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := h.HandleSynced(context.Background(), types.Hash32(types.RandomNodeID()), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrWrongHash)
	})
}

func TestHandler_HandleGossip(t *testing.T) {
	t.Run("malformed data", func(t *testing.T) {
		h := newTestHandler(t)

		err := h.HandleGossip(context.Background(), "peer", []byte("malformed"))
		require.ErrorIs(t, err, malfeasance2.ErrMalformedData)
	})

	t.Run("self peer", func(t *testing.T) {
		h := newTestHandler(t)

		// ignore messages from self
		err := h.HandleGossip(context.Background(), h.self, []byte("malformed"))
		require.NoError(t, err)
	})

	t.Run("unknown version", func(t *testing.T) {
		h := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 42,
		}

		err := h.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownVersion)
	})

	t.Run("unknown domain", func(t *testing.T) {
		h := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  42,
		}

		err := h.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownDomain)
	})

	t.Run("invalid proof", func(t *testing.T) {
		h := newTestHandler(t)
		invalidProof := []byte("invalid")
		handlerError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), invalidProof).Return(types.EmptyNodeID, handlerError)
		mockHandler.EXPECT().ReportLabel().Return("invalidPost")
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   invalidProof,
		}

		err := h.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, handlerError)
	})

	t.Run("valid proof", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabel().Return("invalidPost")
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		h.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := h.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.NoError(t, err)
	})

	t.Run("valid proof for known malicious identity", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabel().Return("invalidPost")
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		h.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}
		proofBytes := codec.MustEncode(proof)

		err := malfeasance.AddProof(h.db, nodeID, nil, proofBytes, byte(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		err = h.HandleGossip(context.Background(), "peer", proofBytes)
		require.NoError(t, err)
	})
}
