package malfeasance2_test

import (
	"context"
	"errors"
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
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
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

type testHandler struct {
	*malfeasance2.Handler

	observedLogs *observer.ObservedLogs
	ctrl         *gomock.Controller
	db           sql.StateDatabase
	self         p2p.Peer
	mockTrt      *malfeasance2.Mocktortoise
}

func newTestHandler(tb testing.TB) *testHandler {
	db := statesql.InMemory()

	observer, observedLogs := observer.New(zap.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	ctrl := gomock.NewController(tb)
	mockTrt := malfeasance2.NewMocktortoise(ctrl)
	mockFetch := mocks.NewMockFetcher(ctrl)

	h := malfeasance2.NewHandler(
		db,
		logger,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		mockFetch,
		mockTrt,
	)
	return &testHandler{
		Handler: h,

		observedLogs: observedLogs,
		ctrl:         ctrl,
		db:           db,
		self:         "self",
		mockTrt:      mockTrt,
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

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 1
`
		require.NoError(t, testutil.CollectAndCompare(h.NumMalProof(), strings.NewReader(expected)))
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
		mockHandler.EXPECT().ReportLabels(invalidProof).Return([]string{"ATX", "invalidPost"})
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   invalidProof,
		}

		err := h.HandleSynced(context.Background(), types.EmptyHash32, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, handlerError)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="ATX",type="invalidPost"} 1
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 0
`
		require.NoError(t, testutil.CollectAndCompare(h.NumInvalidProofs(), strings.NewReader(expected)))
	})

	t.Run("valid proof", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		h.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := h.HandleSynced(context.Background(), types.Hash32(nodeID), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(h.NumValidProofs(), strings.NewReader(expected)))

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
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		expectedHash := types.RandomHash()
		err := h.HandleSynced(context.Background(), expectedHash, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrWrongHash)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		require.Equal(t, 1, h.observedLogs.Len())
		log := h.observedLogs.All()[0]
		require.Equal(t, zap.WarnLevel, log.Level)
		require.Contains(t, log.Message, "malfeasance proof for wrong identity")
		require.Equal(t, expectedHash.ShortString(), log.ContextMap()["expected"])
		require.Equal(t, p2p.Peer("peer").String(), log.ContextMap()["peer"])

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="ATX",type="invalidPost"} 1
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 0
`
		require.NoError(t, testutil.CollectAndCompare(h.NumInvalidProofs(), strings.NewReader(expected)))
	})
}

func TestHandler_HandleGossip(t *testing.T) {
	t.Run("malformed data", func(t *testing.T) {
		h := newTestHandler(t)

		err := h.HandleGossip(context.Background(), "peer", []byte("malformed"))
		require.ErrorIs(t, err, malfeasance2.ErrMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 1
`
		require.NoError(t, testutil.CollectAndCompare(h.NumMalProof(), strings.NewReader(expected)))
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
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("unknown domain", func(t *testing.T) {
		h := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  42,
		}

		err := h.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownDomain)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("invalid proof", func(t *testing.T) {
		h := newTestHandler(t)
		invalidProof := []byte("invalid")
		handlerError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), invalidProof).Return(types.EmptyNodeID, handlerError)
		mockHandler.EXPECT().ReportLabels(invalidProof).Return([]string{"ATX", "invalidPost"})
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   invalidProof,
		}

		err := h.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, handlerError)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="ATX",type="invalidPost"} 1
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 0
`
		require.NoError(t, testutil.CollectAndCompare(h.NumInvalidProofs(), strings.NewReader(expected)))
	})

	t.Run("valid proof", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		h.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := h.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(h.NumValidProofs(), strings.NewReader(expected)))
		require.NoError(t, err)
	})

	t.Run("valid proof for known malicious identity", func(t *testing.T) {
		h := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(gomock.NewController(t))
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		h.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		h.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}
		proofBytes := codec.MustEncode(proof)

		err := malfeasance.AddProof(h.db, nodeID, nil, proofBytes, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		err = h.HandleGossip(context.Background(), "peer", proofBytes)
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(h.NumValidProofs(), strings.NewReader(expected)))
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
		err := malfeasance.AddProof(h.db, nodeID, nil, proofBytes, 999, time.Now())
		require.NoError(t, err)

		info, err := h.Info(context.Background(), nodeID)
		require.ErrorContains(t, err, "unknown malfeasance domain 999")
		require.Nil(t, info)
	})

	t.Run("invalid proof", func(t *testing.T) {
		h := newTestHandler(t)
		invalidProof := []byte("invalid")
		infoError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(h.ctrl)
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
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(h.ctrl)
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
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(h.ctrl)
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
