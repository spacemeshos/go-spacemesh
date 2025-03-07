package malfeasance2_test

import (
	"bytes"
	"context"
	"errors"
	"sort"
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
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/malfeasance2"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

type testPublisher struct {
	*malfeasance2.Publisher

	observedLogs *observer.ObservedLogs
	db           sql.StateDatabase
	mockSync     *malfeasance2.Mocksyncer
	mockTrt      *malfeasance2.Mocktortoise
	mockPub      *mocks.MockPublisher
}

func newTestPublisher(tb testing.TB) *testPublisher {
	db := statesql.InMemory()

	observer, observedLogs := observer.New(zap.DebugLevel)
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
		db:           db,
		mockSync:     mockSync,
		mockTrt:      mockTrt,
		mockPub:      mockPub,
	}
}

func TestPublishATXProof(t *testing.T) {
	t.Run("not married and in sync", func(t *testing.T) {
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

		events.InitializeReporter()
		defer events.CloseEventReporter()
		sub, err := events.SubscribeMatched(func(event *events.EventMalfeasance) bool { return true })
		require.NoError(t, err)
		defer sub.Close()

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof))

		err = tp.PublishATXProof(t.Context(), nodeID, proof, false)
		require.NoError(t, err)

		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		select {
		case ev := <-sub.Out():
			require.Equal(t, nodeID, ev.Smesher)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "timed out waiting for event")
		}
	})

	t.Run("not married and in sync, allow without refATXs", func(t *testing.T) {
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()

		malfeasanceProof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   proof,
		}

		events.InitializeReporter()
		defer events.CloseEventReporter()
		sub, err := events.SubscribeMatched(func(event *events.EventMalfeasance) bool { return true })
		require.NoError(t, err)
		defer sub.Close()

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof))

		err = tp.PublishATXProof(t.Context(), nodeID, proof, true)
		require.NoError(t, err)

		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		select {
		case ev := <-sub.Out():
			require.Equal(t, nodeID, ev.Smesher)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "timed out waiting for event")
		}
	})

	t.Run("not married, in sync, but failed to gossip", func(t *testing.T) {
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

		events.InitializeReporter()
		defer events.CloseEventReporter()
		sub, err := events.SubscribeMatched(func(event *events.EventMalfeasance) bool { return true })
		require.NoError(t, err)
		defer sub.Close()

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		errPublish := errors.New("failed to publish")
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof)).
			Return(errPublish)

		err = tp.PublishATXProof(t.Context(), nodeID, proof, false)
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

		select {
		case ev := <-sub.Out():
			require.Equal(t, nodeID, ev.Smesher)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "timed out waiting for event")
		}
	})

	t.Run("not married, not in sync", func(t *testing.T) {
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		events.InitializeReporter()
		defer events.CloseEventReporter()
		sub, err := events.SubscribeMatched(func(event *events.EventMalfeasance) bool { return true })
		require.NoError(t, err)
		defer sub.Close()

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(false) // results in no gossip but only storing the proof

		err = tp.PublishATXProof(t.Context(), nodeID, proof, false)
		require.NoError(t, err)

		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		select {
		case ev := <-sub.Out():
			require.Equal(t, nodeID, ev.Smesher)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "timed out waiting for event")
		}
	})

	t.Run("not married, not in sync, allow no ref ATXs", func(t *testing.T) {
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()

		events.InitializeReporter()
		defer events.CloseEventReporter()
		sub, err := events.SubscribeMatched(func(event *events.EventMalfeasance) bool { return true })
		require.NoError(t, err)
		defer sub.Close()

		tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		tp.mockSync.EXPECT().ListenToATXGossip().Return(false) // results in no gossip but only storing the proof

		err = tp.PublishATXProof(t.Context(), nodeID, proof, true)
		require.NoError(t, err)

		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		select {
		case ev := <-sub.Out():
			require.Equal(t, nodeID, ev.Smesher)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "timed out waiting for event")
		}
	})

	t.Run("married and in sync", func(t *testing.T) {
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(mATXID)
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		mID, err := marriage.NewID(tp.db)
		require.NoError(t, err)

		refATXs := []types.ATXID{atx.ID()}
		for i := range nodeIDs {
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           mATXID,
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
		}
		for i := range 10 {
			nodeID := types.RandomNodeID()
			atx := &types.ActivationTx{
				SmesherID: nodeID,
			}
			atx.SetID(types.RandomATXID())
			require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeID,
				ATX:           atx.ID(),
				MarriageIndex: i,
				Target:        nodeIDs[0],
			}))
			refATXs = append(refATXs, atx.ID())
			nodeIDs = append(nodeIDs, nodeID)
		}

		malfeasanceProof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: refATXs,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   proof,
		}

		events.InitializeReporter()
		defer events.CloseEventReporter()
		sub, err := events.SubscribeMatched(func(event *events.EventMalfeasance) bool { return true })
		require.NoError(t, err)
		defer sub.Close()

		for _, nodeID := range nodeIDs {
			tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		}
		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, gomock.Any()).DoAndReturn(
			func(ctx context.Context, topic string, data []byte) error {
				var got malfeasance2.MalfeasanceProof
				codec.MustDecode(data, &got)
				sort.Slice(malfeasanceProof.RefATXs, func(i, j int) bool {
					return bytes.Compare(malfeasanceProof.RefATXs[i].Bytes(), malfeasanceProof.RefATXs[j].Bytes()) < 0
				})
				sort.Slice(got.RefATXs, func(i, j int) bool {
					return bytes.Compare(got.RefATXs[i].Bytes(), got.RefATXs[j].Bytes()) < 0
				})
				require.Equal(t, malfeasanceProof, &got)
				return nil
			},
		)

		err = tp.PublishATXProof(t.Context(), nodeIDs[2], proof, false)
		require.NoError(t, err)

		for i := range nodeIDs {
			malicious, err := malfeasance.IsMalicious(tp.db, nodeIDs[i])
			require.NoError(t, err)
			require.True(t, malicious)
		}
		dbProof, domain, err := malfeasance.MarriageProof(tp.db, mID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		eventNodeIDs := make([]types.NodeID, 0, len(nodeIDs))
		for range nodeIDs {
			select {
			case ev := <-sub.Out():
				eventNodeIDs = append(eventNodeIDs, ev.Smesher)
			case <-time.After(5 * time.Second):
				require.FailNow(t, "timed out waiting for event")
			}
		}
		require.ElementsMatch(t, nodeIDs, eventNodeIDs)
	})

	t.Run("identity already malicious", func(t *testing.T) {
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		err := malfeasance.AddProof(tp.db, nodeID, nil, proof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		err = tp.PublishATXProof(t.Context(), nodeID, proof, false)
		require.NoError(t, err)

		dbProof, domain, err := malfeasance.NodeIDProof(tp.db, nodeID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		logs := tp.observedLogs.FilterLevelExact(zap.DebugLevel)

		require.Equal(t, 2, logs.Len())
		require.Equal(t, zap.DebugLevel, logs.All()[0].Level)
		require.Contains(t, logs.All()[0].Message, "smesher is already marked as malicious")
		require.Equal(t, zap.DebugLevel, logs.All()[1].Level)
		require.Contains(t, logs.All()[1].Message, "persisted malfeasance proof")
	})

	t.Run("married and all already malicious", func(t *testing.T) {
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(mATXID)
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		mID, err := marriage.NewID(tp.db)
		require.NoError(t, err)

		refATXs := []types.ATXID{atx.ID()}
		for i := range nodeIDs {
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           mATXID,
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
			if i == 0 {
				require.NoError(t, malfeasance.AddProof(
					tp.db,
					nodeIDs[i],
					&mID,
					proof,
					int(malfeasance2.InvalidActivation),
					time.Now(),
				))
				continue
			}
			require.NoError(t, malfeasance.SetMalicious(tp.db, nodeIDs[i], mID, time.Now()))
		}
		for i := range 10 {
			nodeID := types.RandomNodeID()
			atx := &types.ActivationTx{
				SmesherID: nodeID,
			}
			atx.SetID(types.RandomATXID())
			require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeID,
				ATX:           atx.ID(),
				MarriageIndex: i,
				Target:        nodeIDs[0],
			}))
			refATXs = append(refATXs, atx.ID())
			nodeIDs = append(nodeIDs, nodeID)
			require.NoError(t, malfeasance.SetMalicious(tp.db, nodeID, mID, time.Now()))
		}

		err = tp.PublishATXProof(t.Context(), nodeIDs[2], proof, false)
		require.NoError(t, err)

		for i := range nodeIDs {
			malicious, err := malfeasance.IsMalicious(tp.db, nodeIDs[i])
			require.NoError(t, err)
			require.True(t, malicious)
		}
		dbProof, domain, err := malfeasance.MarriageProof(tp.db, mID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		logs := tp.observedLogs.FilterLevelExact(zap.DebugLevel)

		require.Equal(t, 31, logs.Len())
		require.Equal(t, zap.DebugLevel, logs.All()[0].Level)
		for i := range nodeIDs {
			require.Contains(t, logs.All()[i].Message, "smesher is already marked as malicious")
		}
		require.Equal(t, zap.DebugLevel, logs.All()[30].Level)
		require.Contains(t, logs.All()[30].Message, "persisted malfeasance proof")
	})

	t.Run("married and some already malicious", func(t *testing.T) {
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(mATXID)
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		mID, err := marriage.NewID(tp.db)
		require.NoError(t, err)

		refATXs := []types.ATXID{atx.ID()}
		for i := range nodeIDs {
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           mATXID,
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
			if i == 0 {
				require.NoError(t, malfeasance.AddProof(
					tp.db,
					nodeIDs[i],
					&mID,
					proof,
					int(malfeasance2.InvalidActivation),
					time.Now()),
				)
				continue
			}
			require.NoError(t, malfeasance.SetMalicious(tp.db, nodeIDs[i], mID, time.Now()))
		}
		for i := range 10 {
			nodeID := types.RandomNodeID()
			atx := &types.ActivationTx{
				SmesherID: nodeID,
			}
			atx.SetID(types.RandomATXID())
			require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeID,
				ATX:           atx.ID(),
				MarriageIndex: i,
				Target:        nodeIDs[0],
			}))
			refATXs = append(refATXs, atx.ID())
			nodeIDs = append(nodeIDs, nodeID)
		}

		malfeasanceProof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: refATXs,
			Domain:  malfeasance2.InvalidActivation,
			Proof:   proof,
		}

		events.InitializeReporter()
		defer events.CloseEventReporter()
		sub, err := events.SubscribeMatched(func(event *events.EventMalfeasance) bool { return true })
		require.NoError(t, err)
		defer sub.Close()

		for _, nodeID := range nodeIDs { // only the last 10 were not already marked as malicious
			tp.mockTrt.EXPECT().OnMalfeasance(nodeID)
		}
		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, gomock.Any()).DoAndReturn(
			func(ctx context.Context, topic string, data []byte) error {
				var got malfeasance2.MalfeasanceProof
				codec.MustDecode(data, &got)
				sort.Slice(malfeasanceProof.RefATXs, func(i, j int) bool {
					return bytes.Compare(malfeasanceProof.RefATXs[i].Bytes(), malfeasanceProof.RefATXs[j].Bytes()) < 0
				})
				sort.Slice(got.RefATXs, func(i, j int) bool {
					return bytes.Compare(got.RefATXs[i].Bytes(), got.RefATXs[j].Bytes()) < 0
				})
				require.Equal(t, malfeasanceProof, &got)
				return nil
			},
		)

		err = tp.PublishATXProof(t.Context(), nodeIDs[2], proof, false)
		require.NoError(t, err)

		for i := range nodeIDs {
			malicious, err := malfeasance.IsMalicious(tp.db, nodeIDs[i])
			require.NoError(t, err)
			require.True(t, malicious)
		}
		dbProof, domain, err := malfeasance.MarriageProof(tp.db, mID)
		require.NoError(t, err)
		require.Equal(t, malfeasance2.InvalidActivation, malfeasance2.ProofDomain(domain))
		require.Equal(t, proof, dbProof)

		logs := tp.observedLogs.FilterLevelExact(zap.DebugLevel)

		require.Equal(t, 22, logs.Len())
		require.Equal(t, zap.DebugLevel, logs.All()[0].Level)
		for i := range nodeIDs[:20] {
			// first 20 were already malicious
			require.Contains(t, logs.All()[i].Message, "smesher is already marked as malicious")
		}
		require.Equal(t, zap.DebugLevel, logs.All()[20].Level)
		require.Contains(t, logs.All()[20].Message, "persisted malfeasance proof")
		require.Equal(t, zap.DebugLevel, logs.All()[21].Level)
		require.Contains(t, logs.All()[21].Message, "broadcast malfeasance proof")

		eventNodeIDs := make([]types.NodeID, 0, len(nodeIDs))
		for range nodeIDs {
			select {
			case ev := <-sub.Out():
				eventNodeIDs = append(eventNodeIDs, ev.Smesher)
			case <-time.After(5 * time.Second):
				require.FailNow(t, "timed out waiting for event")
			}
		}
		require.ElementsMatch(t, nodeIDs, eventNodeIDs)
	})
}

func TestRegossip(t *testing.T) {
	t.Parallel()

	t.Run("not married and in sync", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		err := malfeasance.AddProof(tp.db, nodeID, nil, proof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		malfeasanceProof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atx.ID()},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   proof,
		}

		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof))

		err = tp.Regossip(t.Context(), nodeID)
		require.NoError(t, err)
	})

	t.Run("not married and not in sync", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeID := types.RandomNodeID()
		atx := &types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		err := malfeasance.AddProof(tp.db, nodeID, nil, proof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		tp.mockSync.EXPECT().ListenToATXGossip().Return(false)

		err = tp.Regossip(t.Context(), nodeID)
		require.NoError(t, err)
	})

	t.Run("married and in sync", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(mATXID)
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		mID, err := marriage.NewID(tp.db)
		require.NoError(t, err)

		for i := range nodeIDs {
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           mATXID,
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
			if i == 0 {
				require.NoError(t, malfeasance.AddProof(
					tp.db,
					nodeIDs[i],
					&mID,
					proof,
					int(malfeasance2.InvalidActivation),
					time.Now(),
				))
				continue
			}
			require.NoError(t, malfeasance.SetMalicious(tp.db, nodeIDs[i], mID, time.Now()))
		}

		malfeasanceProof := &malfeasance2.MalfeasanceProof{
			Version: 0,
			RefATXs: []types.ATXID{atx.ID()},
			Domain:  malfeasance2.InvalidActivation,
			Proof:   proof,
		}

		tp.mockSync.EXPECT().ListenToATXGossip().Return(true)
		tp.mockPub.EXPECT().Publish(gomock.Any(), pubsub.MalfeasanceProof2, codec.MustEncode(malfeasanceProof))

		err = tp.Regossip(t.Context(), nodeIDs[1])
		require.NoError(t, err)
	})

	t.Run("married and not in sync", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(mATXID)
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))

		mID, err := marriage.NewID(tp.db)
		require.NoError(t, err)

		for i := range nodeIDs {
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           mATXID,
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
			if i == 0 {
				require.NoError(t, malfeasance.AddProof(
					tp.db,
					nodeIDs[i],
					&mID,
					proof,
					int(malfeasance2.InvalidActivation),
					time.Now(),
				))
				continue
			}
			require.NoError(t, malfeasance.SetMalicious(tp.db, nodeIDs[i], mID, time.Now()))
		}

		tp.mockSync.EXPECT().ListenToATXGossip().Return(false)

		err = tp.Regossip(t.Context(), nodeIDs[1])
		require.NoError(t, err)
	})
}

func TestProofByID(t *testing.T) {
	t.Run("not married no proof", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		nodeID := types.RandomNodeID()

		proofBytes, err := tp.ProofByID(t.Context(), nodeID)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, proofBytes)
	})

	t.Run("not married with proof", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		nodeID := types.RandomNodeID()
		atx := types.ActivationTx{
			SmesherID: nodeID,
		}
		atx.SetID(types.RandomATXID())
		atxs.Add(tp.db, &atx, types.AtxBlob{})
		proof := types.RandomBytes(10)
		err := malfeasance.AddProof(tp.db, nodeID, nil, proof, int(malfeasance2.InvalidActivation), time.Now())
		require.NoError(t, err)

		proofBytes, err := tp.ProofByID(t.Context(), nodeID)
		require.NoError(t, err)

		var malProof malfeasance2.MalfeasanceProof
		require.NoError(t, codec.Decode(proofBytes, &malProof))
		require.Equal(t, proof, malProof.Proof)
		require.Equal(t, malfeasance2.InvalidActivation, malProof.Domain)
		require.Len(t, malProof.RefATXs, 1)
		require.Equal(t, atx.ID(), malProof.RefATXs[0])
	})

	t.Run("married no proof", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		mATXID := types.RandomATXID()
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(mATXID)
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))
		mATX2ID := types.RandomATXID()
		atx2 := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx2.SetID(mATX2ID)
		require.NoError(t, atxs.Add(tp.db, atx2, types.AtxBlob{}))

		mID, err := marriage.NewID(tp.db)
		require.NoError(t, err)

		for i := range nodeIDs {
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           mATXID,
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
		}

		proofBytes, err := tp.ProofByID(t.Context(), nodeIDs[4])
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.Nil(t, proofBytes)
	})

	t.Run("married with proof", func(t *testing.T) {
		t.Parallel()
		tp := newTestPublisher(t)
		proof := types.RandomBytes(10)
		nodeIDs := make([]types.NodeID, 20)
		for i := range nodeIDs {
			nodeIDs[i] = types.RandomNodeID()
		}
		atx := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx, types.AtxBlob{}))
		atx2 := &types.ActivationTx{
			SmesherID: nodeIDs[0],
		}
		atx2.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tp.db, atx2, types.AtxBlob{}))

		mID, err := marriage.NewID(tp.db)
		require.NoError(t, err)

		for i := range nodeIDs[:10] {
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i],
				ATX:           atx.ID(),
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
			if i == 0 {
				require.NoError(t, malfeasance.AddProof(
					tp.db,
					nodeIDs[i],
					&mID,
					proof,
					int(malfeasance2.InvalidActivation),
					time.Now()),
				)
				continue
			}
			require.NoError(t, malfeasance.SetMalicious(tp.db, nodeIDs[i], mID, time.Now()))
		}
		for i := range nodeIDs[10:] { // smesher has married twice
			require.NoError(t, marriage.Add(tp.db, marriage.Info{
				ID:            mID,
				NodeID:        nodeIDs[i+10],
				ATX:           atx2.ID(),
				MarriageIndex: i,
				Target:        nodeIDs[0],
				Signature:     types.RandomEdSignature(),
			}))
			require.NoError(t, malfeasance.SetMalicious(tp.db, nodeIDs[i], mID, time.Now()))
		}

		proofBytes, err := tp.ProofByID(t.Context(), nodeIDs[4])
		require.NoError(t, err)

		var malProof malfeasance2.MalfeasanceProof
		require.NoError(t, codec.Decode(proofBytes, &malProof))
		require.Equal(t, proof, malProof.Proof)
		require.Equal(t, malfeasance2.InvalidActivation, malProof.Domain)
		require.ElementsMatch(t, []types.ATXID{atx.ID(), atx2.ID()}, malProof.RefATXs)
	})
}
