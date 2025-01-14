package malfeasance2_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance2"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testPublisher struct {
	*malfeasance2.Publisher

	observedLogs *observer.ObservedLogs
	ctrl         *gomock.Controller
	db           sql.StateDatabase
	mockSync     *malfeasance2.Mocksyncer
	mockTrt      *malfeasance2.Mocktortoise
	mockPub      *mocks.MockPublisher
}

func newTestPublisher(tb testing.TB) *testPublisher {
	db := statesql.InMemory()

	observer, observedLogs := observer.New(zap.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	ctrl := gomock.NewController(tb)
	mockSync := malfeasance2.NewMocksyncer(ctrl)
	mockTrt := malfeasance2.NewMocktortoise(ctrl)
	mockPub := mocks.NewMockPublisher(ctrl)

	tp := malfeasance2.NewPublisher(
		logger,
		db,
		mockSync,
		mockTrt,
		mockPub,
	)
	return &testPublisher{
		Publisher: tp,

		observedLogs: observedLogs,
		ctrl:         ctrl,
		db:           db,
		mockSync:     mockSync,
		mockTrt:      mockTrt,
		mockPub:      mockPub,
	}
}

func TestPublishATXProof(t *testing.T) {
	t.Parallel()

	t.Run("valid proof and in sync", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		malfeasanceProof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atx.ID()},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   proof,
		}

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof))

		err := tp.PublishATXProof(context.Background(), nodeID, proof)
		require.NoError(t, err)

		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)
	})

	t.Run("valid proof, in sync, but failed to publish", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		malfeasanceProof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atx.ID()},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   proof,
		}

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		errPublish := errors.New("failed to publish")
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof)).
			Return(errPublish)

		err := tp.PublishATXProof(context.Background(), nodeID, proof)
		require.ErrorIs(t, err, errPublish)

		logs := tp.observedLogs.FilterLevelExact(zap.ErrorLevel)

		require.Equal(t, 1, logs.Len())
		require.Equal(t, zap.ErrorLevel, logs.All()[0].Level)
		require.Contains(t, logs.All()[0].Message, "failed to broadcast malfeasance proof")

		// proof still stored
		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)
	})

	t.Run("valid proof, not in sync", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(false) // results in no gossip but only storing the proof

		err := tp.PublishATXProof(context.Background(), nodeID, proof)
		require.NoError(t, err)

		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)
	})
}
