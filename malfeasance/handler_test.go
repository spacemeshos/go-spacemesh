package malfeasance

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

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testMalfeasanceHandler struct {
	*Handler

	observedLogs *observer.ObservedLogs
	db           sql.StateDatabase
	mockTrt      *Mocktortoise
}

func newHandler(tb testing.TB) *testMalfeasanceHandler {
	db := statesql.InMemory()
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	ctrl := gomock.NewController(tb)
	trt := NewMocktortoise(ctrl)
	store := atxsdata.New()
	h := NewHandler(
		datastore.NewCachedDB(db, logger, datastore.WithConsensusCache(store)),
		logger,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		trt,
	)

	return &testMalfeasanceHandler{
		Handler: h,

		observedLogs: observedLogs,
		db:           db,
		mockTrt:      trt,
	}
}

func TestHandler_HandleMalfeasanceProof(t *testing.T) {
	t.Run("malformed data", func(t *testing.T) {
		h := newHandler(t)

		err := h.HandleMalfeasanceProof(context.Background(), "peer", []byte{0x01})
		require.ErrorIs(t, err, errMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("unknown malfeasance type", func(t *testing.T) {
		h := newHandler(t)

		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: types.LayerID(22),
				Proof: wire.Proof{
					Type: wire.MultipleATXs,
					Data: &wire.AtxProof{},
				},
			},
		}

		err := h.HandleMalfeasanceProof(context.Background(), "peer", codec.MustEncode(gossip))
		require.ErrorIs(t, err, errUnknownProof)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("invalid proof", func(t *testing.T) {
		h := newHandler(t)

		ctrl := gomock.NewController(t)
		handler := NewMockHandlerV1(ctrl)
		handler.EXPECT().Validate(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
				require.IsType(t, &wire.AtxProof{}, data)
				return types.EmptyNodeID, errors.New("invalid proof")
			},
		)
		handler.EXPECT().ReportInvalidProof(gomock.Any())
		h.RegisterHandler(MultipleATXs, handler)

		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: types.LayerID(22),
				Proof: wire.Proof{
					Type: wire.MultipleATXs,
					Data: &wire.AtxProof{},
				},
			},
		}

		err := h.HandleMalfeasanceProof(context.Background(), "peer", codec.MustEncode(gossip))
		require.ErrorContains(t, err, "invalid proof")
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("valid proof", func(t *testing.T) {
		h := newHandler(t)

		nodeID := types.RandomNodeID()
		ctrl := gomock.NewController(t)
		handler := NewMockHandlerV1(ctrl)
		handler.EXPECT().Validate(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
				require.IsType(t, &wire.AtxProof{}, data)
				return nodeID, nil
			},
		)
		handler.EXPECT().ReportProof(gomock.Any())
		h.RegisterHandler(MultipleATXs, handler)

		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: types.LayerID(22),
				Proof: wire.Proof{
					Type: wire.MultipleATXs,
					Data: &wire.AtxProof{},
				},
			},
		}

		h.mockTrt.EXPECT().OnMalfeasance(nodeID)
		err := h.HandleMalfeasanceProof(context.Background(), "peer", codec.MustEncode(gossip))
		require.NoError(t, err)

		malProof, err := identities.GetMalfeasanceProof(h.db, nodeID)
		require.NoError(t, err)
		require.NotEqual(t, gossip.MalfeasanceProof, *malProof)
	})

	t.Run("new proof is noop", func(t *testing.T) {
		h := newHandler(t)

		nodeID := types.RandomNodeID()
		proof := &wire.MalfeasanceProof{
			Layer: types.LayerID(22),
			Proof: wire.Proof{
				Type: wire.MultipleBallots,
				Data: &wire.BallotProof{},
			},
		}
		identities.SetMalicious(h.db, nodeID, codec.MustEncode(proof), time.Now())

		ctrl := gomock.NewController(t)
		handler := NewMockHandlerV1(ctrl)
		handler.EXPECT().Validate(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
				require.IsType(t, &wire.AtxProof{}, data)
				return nodeID, nil
			},
		)
		h.RegisterHandler(MultipleATXs, handler)

		gossip := &wire.MalfeasanceGossip{
			MalfeasanceProof: wire.MalfeasanceProof{
				Layer: types.LayerID(22),
				Proof: wire.Proof{
					Type: wire.MultipleATXs,
					Data: &wire.AtxProof{},
				},
			},
		}

		err := h.HandleMalfeasanceProof(context.Background(), "peer", codec.MustEncode(gossip))
		require.ErrorIs(t, ErrKnownProof, err)

		malProof, err := identities.GetMalfeasanceProof(h.db, nodeID)
		require.NoError(t, err)
		malProof.SetReceived(time.Time{})
		require.Equal(t, proof, malProof)
	})
}

func TestHandler_HandleSyncedMalfeasanceProof(t *testing.T) {
	t.Run("malformed data", func(t *testing.T) {
		h := newHandler(t)

		err := h.HandleSyncedMalfeasanceProof(
			context.Background(),
			types.RandomHash(),
			"peer",
			[]byte{0x01},
		)
		require.ErrorIs(t, err, errMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("unknown malfeasance type", func(t *testing.T) {
		h := newHandler(t)

		proof := &wire.MalfeasanceProof{
			Layer: types.LayerID(22),
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		err := h.HandleSyncedMalfeasanceProof(
			context.Background(),
			types.RandomHash(),
			"peer",
			codec.MustEncode(proof),
		)
		require.ErrorIs(t, err, errUnknownProof)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("valid proof for wrong nodeID", func(t *testing.T) {
		h := newHandler(t)

		nodeID := types.RandomNodeID()
		ctrl := gomock.NewController(t)
		handler := NewMockHandlerV1(ctrl)
		handler.EXPECT().Validate(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
				require.IsType(t, &wire.AtxProof{}, data)
				return nodeID, nil
			},
		)
		handler.EXPECT().ReportProof(gomock.Any())
		h.RegisterHandler(MultipleATXs, handler)

		proof := &wire.MalfeasanceProof{
			Layer: types.LayerID(22),
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		h.mockTrt.EXPECT().OnMalfeasance(nodeID)
		err := h.HandleSyncedMalfeasanceProof(
			context.Background(),
			types.RandomHash(),
			"peer",
			codec.MustEncode(proof),
		)
		require.ErrorIs(t, err, errWrongHash)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("invalid proof", func(t *testing.T) {
		h := newHandler(t)

		nodeID := types.RandomNodeID()
		ctrl := gomock.NewController(t)
		handler := NewMockHandlerV1(ctrl)
		handler.EXPECT().Validate(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
				require.IsType(t, &wire.AtxProof{}, data)
				return types.EmptyNodeID, errors.New("invalid proof")
			},
		)
		handler.EXPECT().ReportInvalidProof(gomock.Any())
		h.RegisterHandler(MultipleATXs, handler)

		proof := &wire.MalfeasanceProof{
			Layer: types.LayerID(22),
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		err := h.HandleSyncedMalfeasanceProof(
			context.Background(),
			types.Hash32(nodeID),
			"peer",
			codec.MustEncode(proof),
		)
		require.ErrorContains(t, err, "invalid proof")
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("valid proof", func(t *testing.T) {
		h := newHandler(t)

		nodeID := types.RandomNodeID()
		ctrl := gomock.NewController(t)
		handler := NewMockHandlerV1(ctrl)
		handler.EXPECT().Validate(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
				require.IsType(t, &wire.AtxProof{}, data)
				return nodeID, nil
			},
		)
		handler.EXPECT().ReportProof(gomock.Any())
		h.RegisterHandler(MultipleATXs, handler)

		proof := &wire.MalfeasanceProof{
			Layer: types.LayerID(22),
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		h.mockTrt.EXPECT().OnMalfeasance(nodeID)
		err := h.HandleSyncedMalfeasanceProof(
			context.Background(),
			types.Hash32(nodeID),
			"peer",
			codec.MustEncode(proof),
		)
		require.NoError(t, err)

		malProof, err := identities.GetMalfeasanceProof(h.db, nodeID)
		require.NoError(t, err)
		require.NotEqual(t, proof, *malProof)
	})

	t.Run("new proof is noop", func(t *testing.T) {
		h := newHandler(t)

		nodeID := types.RandomNodeID()
		proof := &wire.MalfeasanceProof{
			Layer: types.LayerID(22),
			Proof: wire.Proof{
				Type: wire.MultipleBallots,
				Data: &wire.BallotProof{},
			},
		}
		identities.SetMalicious(h.db, nodeID, codec.MustEncode(proof), time.Now())

		ctrl := gomock.NewController(t)
		handler := NewMockHandlerV1(ctrl)
		handler.EXPECT().Validate(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, data wire.ProofData) (types.NodeID, error) {
				require.IsType(t, &wire.AtxProof{}, data)
				return nodeID, nil
			},
		)
		h.RegisterHandler(MultipleATXs, handler)

		newProof := &wire.MalfeasanceProof{
			Layer: types.LayerID(22),
			Proof: wire.Proof{
				Type: wire.MultipleATXs,
				Data: &wire.AtxProof{},
			},
		}

		err := h.HandleSyncedMalfeasanceProof(
			context.Background(),
			types.Hash32(nodeID),
			"peer",
			codec.MustEncode(newProof),
		)
		require.ErrorIs(t, ErrKnownProof, err)

		malProof, err := identities.GetMalfeasanceProof(h.db, nodeID)
		require.NoError(t, err)
		malProof.SetReceived(time.Time{})
		require.Equal(t, proof, malProof)
	})
}
