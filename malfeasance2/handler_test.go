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
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

type testHandler struct {
	*malfeasance2.Handler

	observedLogs *observer.ObservedLogs
	ctrl         *gomock.Controller
	db           sql.StateDatabase
	self         p2p.Peer
	mockTrt      *malfeasance2.Mocktortoise
	mockFetch    *mocks.MockFetcher
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

	th := malfeasance2.NewHandler(
		db,
		logger,
		"self",
		[]types.NodeID{types.RandomNodeID()},
		mockFetch,
		mockTrt,
	)
	return &testHandler{
		Handler: th,

		observedLogs: observedLogs,
		ctrl:         ctrl,
		db:           db,
		self:         "self",
		mockTrt:      mockTrt,
		mockFetch:    mockFetch,
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

func TestHandler_HandleSync(t *testing.T) {
	t.Parallel()

	t.Run("malformed data", func(t *testing.T) {
		t.Parallel()

		th := newTestHandler(t)

		err := th.HandleSynced(context.Background(), types.EmptyHash32, "peer", []byte("malformed"))
		require.ErrorIs(t, err, malfeasance2.ErrMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumMalProof(), strings.NewReader(expected)))
	})

	t.Run("unknown version", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 42,
		}

		err := th.HandleSynced(context.Background(), types.EmptyHash32, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownVersion)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("unknown domain", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  42,
		}

		err := th.HandleSynced(context.Background(), types.EmptyHash32, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownDomain)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		invalidProof := []byte("invalid")
		handlerError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), invalidProof).Return(types.EmptyNodeID, handlerError)
		mockHandler.EXPECT().ReportLabels(invalidProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   invalidProof,
		}

		err := th.HandleSynced(context.Background(), types.EmptyHash32, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, handlerError)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="ATX",type="invalidPost"} 1
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 0
`
		require.NoError(t, testutil.CollectAndCompare(th.NumInvalidProofs(), strings.NewReader(expected)))
	})

	t.Run("valid proof", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		th.mockTrt.EXPECT().OnMalfeasance(nodeID)
		th.mockFetch.EXPECT().RegisterPeerHashes(p2p.Peer("peer"), []types.Hash32{atxID.Hash32()})
		th.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxID}, gomock.Any()).DoAndReturn(
			// fetch works and stores ATX for that smesher in DB
			func(ctx context.Context, a []types.ATXID, gao ...system.GetAtxOpt) error {
				atx := &types.ActivationTx{
					SmesherID: nodeID,
				}
				atx.SetID(atxID)
				return atxs.Add(th.db, atx, types.AtxBlob{})
			},
		)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atxID},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := th.HandleSynced(context.Background(), types.Hash32(nodeID), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumValidProofs(), strings.NewReader(expected)))

		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("valid proof, married identity", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeIDs[1], nil) // valid for 2nd identity
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		for i := range nodeIDs {
			th.mockTrt.EXPECT().OnMalfeasance(nodeIDs[i])
		}
		th.mockFetch.EXPECT().RegisterPeerHashes(p2p.Peer("peer"), []types.Hash32{mATXID.Hash32()})
		th.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{mATXID}, gomock.Any()).DoAndReturn(
			// fetch works and stores ATXs for that smesher in DB
			func(ctx context.Context, a []types.ATXID, gao ...system.GetAtxOpt) error {
				atx := &types.ActivationTx{
					SmesherID: nodeIDs[0],
				}
				atx.SetID(mATXID)
				if err := atxs.Add(th.db, atx, types.AtxBlob{}); err != nil {
					return err
				}

				mID, err := marriage.NewID(th.db)
				if err != nil {
					return err
				}

				for i := range nodeIDs {
					err = marriage.Add(th.db, marriage.Info{
						ID:            mID,
						NodeID:        nodeIDs[i],
						ATX:           mATXID,
						MarriageIndex: i,
						Target:        nodeIDs[0],
						Signature:     types.RandomEdSignature(),
					})
					if err != nil {
						return err
					}
				}
				return nil
			},
		)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{mATXID},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := th.HandleSynced(context.Background(), types.Hash32(nodeIDs[0]), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumValidProofs(), strings.NewReader(expected)))

		for i := range nodeIDs {
			malicious, err := malfeasance.IsMalicious(th.db, nodeIDs[i])
			require.NoError(t, err)
			require.True(t, malicious)
		}
	})

	t.Run("valid proof, fail to fetch reference ATX", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		th.mockFetch.EXPECT().RegisterPeerHashes(p2p.Peer("peer"), []types.Hash32{atxID.Hash32()})
		errFetchFailed := errors.New("fetch failed")
		th.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxID}, gomock.Any()).Return(errFetchFailed)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atxID},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := th.HandleSynced(context.Background(), types.Hash32(nodeID), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
		require.ErrorIs(t, err, errFetchFailed)

		// not marked malicious since no proof of existence
		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.False(t, malicious)
	})

	t.Run("valid proof, no reference ATX but identity is known", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(atxID)
		require.NoError(t, atxs.Add(th.db, atx, types.AtxBlob{}))
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		th.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			// no reference ATX
			Domain: malfeasance2.InvalidActivation,
			Proof:  validProof,
		}

		err := th.HandleSynced(context.Background(), types.Hash32(nodeID), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumValidProofs(), strings.NewReader(expected)))

		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("valid proof, no reference ATX and identity is unknown", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			// no reference ATX
			Domain: malfeasance2.InvalidActivation,
			Proof:  validProof,
		}

		err := th.HandleSynced(context.Background(), types.Hash32(nodeID), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		// not marked malicious since no proof of existence
		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.False(t, malicious)
	})

	t.Run("valid proof, wrong hash", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(atxID)
		require.NoError(t, atxs.Add(th.db, atx, types.AtxBlob{}))
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		expectedHash := types.RandomHash()
		err := th.HandleSynced(context.Background(), expectedHash, "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrWrongHash)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		require.Equal(t, 1, th.observedLogs.Len())
		log := th.observedLogs.All()[0]
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
		require.NoError(t, testutil.CollectAndCompare(th.NumInvalidProofs(), strings.NewReader(expected)))
	})
}

func TestHandler_HandleGossip(t *testing.T) {
	t.Parallel()

	t.Run("malformed data", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		err := th.HandleGossip(context.Background(), "peer", []byte("malformed"))
		require.ErrorIs(t, err, malfeasance2.ErrMalformedData)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumMalProof(), strings.NewReader(expected)))
	})

	t.Run("self peer", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		// ignore messages from self
		err := th.HandleGossip(context.Background(), th.self, []byte("malformed"))
		require.NoError(t, err)
	})

	t.Run("unknown version", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 42,
		}

		err := th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownVersion)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("unknown domain", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  42,
		}

		err := th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, malfeasance2.ErrUnknownDomain)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		invalidProof := []byte("invalid")
		handlerError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), invalidProof).Return(types.EmptyNodeID, handlerError)
		mockHandler.EXPECT().ReportLabels(invalidProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   invalidProof,
		}

		err := th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, handlerError)
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		expected := `
# HELP spacemesh_malfeasance2_num_invalid_proofs number of invalid malfeasance proofs
# TYPE spacemesh_malfeasance2_num_invalid_proofs counter
spacemesh_malfeasance2_num_invalid_proofs{domain="ATX",type="invalidPost"} 1
spacemesh_malfeasance2_num_invalid_proofs{domain="mal",type="unknown"} 0
`
		require.NoError(t, testutil.CollectAndCompare(th.NumInvalidProofs(), strings.NewReader(expected)))
	})

	t.Run("valid proof", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		th.mockTrt.EXPECT().OnMalfeasance(nodeID)
		th.mockFetch.EXPECT().RegisterPeerHashes(p2p.Peer("peer"), []types.Hash32{atxID.Hash32()})
		th.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxID}, gomock.Any()).DoAndReturn(
			// fetch works and stores ATX for that smesher in DB
			func(ctx context.Context, a []types.ATXID, gao ...system.GetAtxOpt) error {
				atx := &types.ActivationTx{
					SmesherID: nodeID,
				}
				atx.SetID(atxID)
				return atxs.Add(th.db, atx, types.AtxBlob{})
			},
		)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atxID},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumValidProofs(), strings.NewReader(expected)))
		require.NoError(t, err)

		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("valid proof, married identity", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(mATXID)
		require.NoError(t, atxs.Add(th.db, atx, types.AtxBlob{}))

		mID, err := marriage.NewID(th.db)
		require.NoError(t, err)

		for i := range nodeIDs {
			require.NoError(t, marriage.Add(th.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           mATXID,
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
		}
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeIDs[1], nil) // valid for 2nd identity
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		for i := range nodeIDs {
			th.mockTrt.EXPECT().OnMalfeasance(nodeIDs[i])
		}
		th.mockFetch.EXPECT().RegisterPeerHashes(p2p.Peer("peer"), []types.Hash32{mATXID.Hash32()})
		th.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{mATXID}, gomock.Any()).Return(nil)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{mATXID},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err = th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumValidProofs(), strings.NewReader(expected)))

		for i := range nodeIDs {
			malicious, err := malfeasance.IsMalicious(th.db, nodeIDs[i])
			require.NoError(t, err)
			require.True(t, malicious)
		}
	})

	t.Run("valid proof, fail to fetch reference ATX", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		th.mockFetch.EXPECT().RegisterPeerHashes(p2p.Peer("peer"), []types.Hash32{atxID.Hash32()})
		errFetchFailed := errors.New("fetch failed")
		th.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxID}, gomock.Any()).Return(errFetchFailed)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atxID},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}

		err := th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, pubsub.ErrValidationReject)
		require.ErrorIs(t, err, errFetchFailed)

		// not marked malicious since no proof of existence
		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.False(t, malicious)
	})

	t.Run("valid proof, no reference ATX but identity is known", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(atxID)
		require.NoError(t, atxs.Add(th.db, atx, types.AtxBlob{}))
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		th.mockTrt.EXPECT().OnMalfeasance(nodeID)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			// no reference ATX
			Domain: malfeasance2.InvalidActivation,
			Proof:  validProof,
		}

		err := th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumValidProofs(), strings.NewReader(expected)))

		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.True(t, malicious)
	})

	t.Run("valid proof, no reference ATX and identity is unknown", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			// no reference ATX
			Domain: malfeasance2.InvalidActivation,
			Proof:  validProof,
		}

		err := th.HandleGossip(context.Background(), "peer", codec.MustEncode(proof))
		require.ErrorIs(t, err, pubsub.ErrValidationReject)

		// not marked malicious since no proof of existence
		malicious, err := malfeasance.IsMalicious(th.db, nodeID)
		require.NoError(t, err)
		require.False(t, malicious)
	})

	t.Run("valid proof for known malicious identity", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		nodeID := types.RandomNodeID()
		atxID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(atxID)
		require.NoError(t, atxs.Add(th.db, atx, types.AtxBlob{}))
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Validate(gomock.Any(), validProof).Return(nodeID, nil)
		mockHandler.EXPECT().ReportLabels(validProof).Return([]string{"ATX", "invalidPost"})
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)
		th.mockTrt.EXPECT().OnMalfeasance(nodeID)
		th.mockFetch.EXPECT().RegisterPeerHashes(p2p.Peer("peer"), []types.Hash32{atxID.Hash32()})
		th.mockFetch.EXPECT().GetAtxs(gomock.Any(), []types.ATXID{atxID}, gomock.Any()).Return(nil)

		proof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atxID},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   validProof,
		}
		proofBytes := codec.MustEncode(proof)

		err := malfeasance.AddProof(th.db, nodeID, nil, proofBytes, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		err = th.HandleGossip(context.Background(), "peer", proofBytes)
		require.NoError(t, err)

		expected := `
# HELP spacemesh_malfeasance2_num_proofs number of malfeasance proofs
# TYPE spacemesh_malfeasance2_num_proofs counter
spacemesh_malfeasance2_num_proofs{domain="ATX",type="invalidPost"} 1
`
		require.NoError(t, testutil.CollectAndCompare(th.NumValidProofs(), strings.NewReader(expected)))
	})
}

func TestHandler_Info(t *testing.T) {
	t.Parallel()

	t.Run("unknown identity", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		info, err := th.Info(context.Background(), types.RandomNodeID())
		require.ErrorContains(t, err, "get malfeasance proof")
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, info)
	})

	t.Run("married but in good standing", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		nodeID := types.RandomNodeID()
		id, err := marriage.NewID(th.db)
		require.NoError(t, err)

		err = marriage.Add(th.db, marriage.Info{
			ID:            id,
			NodeID:        nodeID,
			ATX:           types.RandomATXID(),
			MarriageIndex: 0,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)

		info, err := th.Info(context.Background(), nodeID)
		require.ErrorContains(t, err, "get malfeasance proof")
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, info)
	})

	t.Run("unknown malfeasance type", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)

		nodeID := types.RandomNodeID()
		proofBytes := types.RandomBytes(100)
		err := malfeasance.AddProof(th.db, nodeID, nil, proofBytes, 999, time.Now())
		require.NoError(t, err)

		info, err := th.Info(context.Background(), nodeID)
		require.ErrorContains(t, err, "unknown malfeasance domain 999")
		require.Nil(t, info)
	})

	t.Run("invalid proof", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		invalidProof := []byte("invalid")
		infoError := errors.New("invalid proof")
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Info(invalidProof).Return(nil, infoError)
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		nodeID := types.RandomNodeID()
		err := malfeasance.AddProof(th.db, nodeID, nil, invalidProof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		info, err := th.Info(context.Background(), nodeID)
		require.ErrorIs(t, err, infoError)
		require.Nil(t, info)
	})

	t.Run("valid proof for node", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		properties := map[string]string{
			"type": "DoubleMarry",
			"key":  "value",
		}
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Info(validProof).Return(properties, nil)
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		nodeID := types.RandomNodeID()
		err := malfeasance.AddProof(th.db, nodeID, nil, validProof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		expectedProperties := maps.Clone(properties)
		expectedProperties["domain"] = strconv.FormatUint(uint64(malfeasance2.InvalidActivation), 10)

		info, err := th.Info(context.Background(), nodeID)
		require.NoError(t, err)
		require.Equal(t, expectedProperties, info)
	})

	t.Run("valid proof for married node", func(t *testing.T) {
		t.Parallel()
		th := newTestHandler(t)
		validProof := []byte("valid")
		properties := map[string]string{
			"type": "InvalidPost",
			"key":  "value",
		}
		mockHandler := malfeasance2.NewMockMalfeasanceHandler(th.ctrl)
		mockHandler.EXPECT().Info(validProof).Return(properties, nil)
		th.RegisterHandler(malfeasance2.InvalidActivation, mockHandler)

		maliciousID := types.RandomNodeID()
		nodeID := types.RandomNodeID()

		id, err := marriage.NewID(th.db)
		require.NoError(t, err)

		err = marriage.Add(th.db, marriage.Info{
			ID:            id,
			NodeID:        maliciousID,
			ATX:           types.RandomATXID(),
			MarriageIndex: 0,
			Target:        types.RandomNodeID(),
			Signature:     types.RandomEdSignature(),
		})
		require.NoError(t, err)

		err = malfeasance.AddProof(
			th.db,
			nodeID,
			&id,
			validProof,
			int(malfeasance2.InvalidActivation),
			time.Now(),
		)
		require.NoError(t, err)

		err = malfeasance.SetMalicious(th.db, maliciousID, id, time.Now())
		require.NoError(t, err)

		expectedProperties := maps.Clone(properties)
		expectedProperties["domain"] = strconv.FormatUint(uint64(malfeasance2.InvalidActivation), 10)
		expectedProperties["malicious_id"] = maliciousID.String()

		info, err := th.Info(context.Background(), maliciousID)
		require.NoError(t, err)
		require.Equal(t, expectedProperties, info)
	})
}
