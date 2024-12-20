package malfeasance2_test

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance2"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testHandler struct {
	*malfeasance2.Handler

	observedLogs *observer.ObservedLogs
	ctrl         *gomock.Controller
	db           sql.StateDatabase
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
		ctrl:         ctrl,
		db:           db,
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()

	t.Run("register", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		handler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		th.RegisterHandler(malfeasance2.InvalidActivation, handler)
	})

	t.Run("already registered", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		handler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		th.RegisterHandler(malfeasance2.InvalidActivation, handler)

		require.Panics(t, func() {
			th.RegisterHandler(malfeasance2.InvalidActivation, handler)
		})

		logs := th.observedLogs.FilterLevelExact(zap.PanicLevel)

		require.Equal(t, 1, logs.Len())
		require.Equal(t, zap.PanicLevel, logs.All()[0].Level)
		require.Contains(t, logs.All()[0].Message, "handler already registered")
	})
}

func TestHandler_Info(t *testing.T) {
	t.Run("unknown identity", func(t *testing.T) {
		h := newTestHandler(t)

		info, err := h.Info(context.Background(), types.RandomNodeID())
		require.ErrorContains(t, err, "get malfeasance proof")
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, info)
	})

	t.Run("unknown malfeasance type", func(t *testing.T) {
		h := newTestHandler(t)

		nodeID := types.RandomNodeID()
		proofBytes := types.RandomBytes(100)
		err := malfeasance.AddProof(h.db, nodeID, nil, proofBytes, 1, time.Now())
		require.NoError(t, err)

		info, err := h.Info(context.Background(), nodeID)
		require.ErrorContains(t, err, "unknown malfeasance domain 1")
		require.Nil(t, info)
	})

	t.Run("invalid proof", func(t *testing.T) {
		h := newTestHandler(t)
		invalidProof := []byte("invalid")
		infoError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Info(invalidProof).Return(nil, infoError)
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		nodeID := types.RandomNodeID()
		err := malfeasance.AddProof(h.db, nodeID, nil, invalidProof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		info, err := h.Info(context.Background(), nodeID)
		require.ErrorIs(t, err, infoError)
		require.Nil(t, info)
	})

	t.Run("valid proof for node", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		properties := map[string]string{
			"type": "DoubleMarry",
			"key":  "value",
		}
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Info(validProof).Return(properties, nil)
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		nodeID := types.RandomNodeID()
		err := malfeasance.AddProof(h.db, nodeID, nil, validProof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		expectedProperties := maps.Clone(properties)
		expectedProperties["domain"] = strconv.FormatUint(uint64(malfeasance2.InvalidActivation), 10)

		info, err := h.Info(context.Background(), nodeID)
		require.NoError(t, err)
		require.Equal(t, expectedProperties, info)
	})

	t.Run("valid proof for married node", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		properties := map[string]string{
			"type": "InvalidPost",
			"key":  "value",
		}
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Info(validProof).Return(properties, nil)
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		maliciousID := types.RandomNodeID()
		nodeID := types.RandomNodeID()

		id, err := marriage.NewID(h.db)
		require.NoError(t, err)

		err = marriage.Add(h.db, marriage.Info{
			ID:            id,
			NodeID:        maliciousID,
			ATX:           types.RandomATXID(),
			MarriageIndex: 0,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)

		err = malfeasance.AddProof(
			h.db,
			nodeID,
			&id,
			validProof,
			int(malfeasance2.InvalidActivation),
			time.Now(),
		)
		require.NoError(t, err)

		err = malfeasance.SetMalicious(h.db, maliciousID, id, time.Now())
		require.NoError(t, err)

		expectedProperties := maps.Clone(properties)
		expectedProperties["domain"] = strconv.FormatUint(uint64(malfeasance2.InvalidActivation), 10)
		expectedProperties["malicious_id"] = maliciousID.String()

		info, err := h.Info(context.Background(), maliciousID)
		require.NoError(t, err)
		require.Equal(t, expectedProperties, info)
	})
}
