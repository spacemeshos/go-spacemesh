package activation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func defaultPoetServiceMock(t *testing.T, ctrl *gomock.Controller, address, roundId string) *MockPoetService {
	t.Helper()
	poet := NewMockPoetService(ctrl)
	poet.EXPECT().
		Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes().
		Return(&types.PoetRound{ID: roundId}, nil)
	poet.EXPECT().Address().AnyTimes().Return(address).AnyTimes()
	return poet
}

func defaultLayerClockMock(ctrl *gomock.Controller) *MocklayerClock {
	mclock := NewMocklayerClock(ctrl)
	mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
		func(got types.LayerID) time.Time {
			genesis := time.Now().Add(-time.Duration(postGenesisEpoch.FirstLayer()) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		},
	)
	return mclock
}

type testNIPostBuilder struct {
	*NIPostBuilder

	observedLogs *observer.ObservedLogs
	eventSub     <-chan events.UserEvent

	mDb          sql.LocalDatabase
	mLogger      *zap.Logger
	mPoetDb      *MockpoetDbAPI
	mClock       *MocklayerClock
	mPostService *MockpostService
	mPostClient  *MockPostClient
	mValidator   *MocknipostValidator
}

func newTestNIPostBuilder(tb testing.TB) *testNIPostBuilder {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	events.InitializeReporter()
	sub, _, err := events.SubscribeUserEvents(events.WithBuffer(10))
	require.NoError(tb, err)
	tb.Cleanup(func() {
		sub.Close()
		_, ok := <-sub.Out()
		require.False(tb, ok, "received unexpected event")
	})

	ctrl := gomock.NewController(tb)
	tnb := &testNIPostBuilder{
		observedLogs: observedLogs,
		eventSub:     sub.Out(),

		mDb:          localsql.InMemory(),
		mPoetDb:      NewMockpoetDbAPI(ctrl),
		mPostService: NewMockpostService(ctrl),
		mPostClient:  NewMockPostClient(ctrl),
		mLogger:      logger,
		mClock:       defaultLayerClockMock(ctrl),
		mValidator:   NewMocknipostValidator(ctrl),
	}

	nb, err := NewNIPostBuilder(
		tnb.mDb,
		tnb.mPostService,
		tnb.mLogger,
		PoetConfig{},
		tnb.mClock,
		tnb.mValidator,
	)
	require.NoError(tb, err)
	tnb.NIPostBuilder = nb
	return tnb
}

func Test_NIPost_PostClientHandling(t *testing.T) {
	t.Run("connect then complete", func(t *testing.T) {
		// post client connects, starts post, then completes successfully
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tnb := newTestNIPostBuilder(t)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(tnb.mPostClient, nil)
		tnb.mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{}, &types.PostInfo{}, nil)

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge, nil)
		require.NoError(t, err)
		require.NotNil(t, nipost)
		require.NotNil(t, nipostInfo)

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostStart()
			require.NotNil(t, event, "wrong event type")
			require.EqualValues(t, shared.ZeroChallenge, event.Challenge)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostComplete()
			require.NotNil(t, event, "wrong event type")
			require.EqualValues(t, shared.ZeroChallenge, event.Challenge)
			require.False(t, e.Event.Failure)
			require.Equal(t, "Node finished PoST execution using PoET challenge.", e.Event.Help)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}
	})

	t.Run("connect then error", func(t *testing.T) {
		// post client connects, starts post, then fails with an error that is not a disconnect
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tnb := newTestNIPostBuilder(t)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(tnb.mPostClient, nil)
		expectedErr := errors.New("some error")
		tnb.mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, expectedErr)

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge, nil)
		require.ErrorIs(t, err, expectedErr)
		require.Nil(t, nipost)
		require.Nil(t, nipostInfo)

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostStart()
			require.NotNil(t, event, "wrong event type")
			require.EqualValues(t, shared.ZeroChallenge, event.Challenge)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostComplete()
			require.NotNil(t, event, "wrong event type")
			require.True(t, e.Event.Failure)
			require.Equal(t, "Node failed PoST execution.", e.Event.Help)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}
	})

	t.Run("connect, disconnect, reconnect then complete", func(t *testing.T) {
		// post client connects, starts post, disconnects in between but completes successfully
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tnb := newTestNIPostBuilder(t)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(tnb.mPostClient, nil)
		tnb.mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, ErrPostClientClosed)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(tnb.mPostClient, nil)
		tnb.mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{}, &types.PostInfo{}, nil)

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge, nil)
		require.NoError(t, err)
		require.NotNil(t, nipost)
		require.NotNil(t, nipostInfo)

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostStart()
			require.NotNil(t, event, "wrong event type")
			require.EqualValues(t, shared.ZeroChallenge, event.Challenge)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostComplete()
			require.NotNil(t, event, "wrong event type")
			require.EqualValues(t, shared.ZeroChallenge, event.Challenge)
			require.False(t, e.Event.Failure)
			require.Equal(t, "Node finished PoST execution using PoET challenge.", e.Event.Help)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}
	})

	t.Run("connect, disconnect, then cancel before reconnect", func(t *testing.T) {
		// post client connects, starts post, disconnects in between and proving is canceled before reconnection
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		tnb := newTestNIPostBuilder(t)

		var eg errgroup.Group
		eg.Go(func() error {
			select {
			case e := <-tnb.eventSub:
				event := e.Event.GetPostStart()
				require.NotNil(t, event, "wrong event type")
				require.EqualValues(t, shared.ZeroChallenge, event.Challenge)
			case <-time.After(5 * time.Second):
				require.Fail(t, "timeout waiting for event")
			}

			cancel()

			select {
			case e := <-tnb.eventSub:
				event := e.Event.GetPostComplete()
				require.NotNil(t, event, "wrong event type")
				require.True(t, e.Event.Failure)
				require.Equal(t, "Node failed PoST execution.", e.Event.Help)
			case <-time.After(5 * time.Second):
				require.Fail(t, "timeout waiting for event")
			}
			return nil
		})

		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(tnb.mPostClient, nil)
		tnb.mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, ErrPostClientClosed)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).DoAndReturn(
			func(types.NodeID) (PostClient, error) {
				<-ctx.Done()
				return nil, ErrPostClientNotConnected
			})

		nipost, nipostInfo, err := tnb.Proof(ctx, sig.NodeID(), shared.ZeroChallenge, nil)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, nipost)
		require.Nil(t, nipostInfo)

		require.NoError(t, eg.Wait())
	})

	t.Run("connect, disconnect, reconnect then error", func(t *testing.T) {
		// post client connects, starts post, disconnects in between and then fails to complete
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tnb := newTestNIPostBuilder(t)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(tnb.mPostClient, nil)
		tnb.mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, ErrPostClientClosed).Times(1)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(tnb.mPostClient, nil)

		expectedErr := errors.New("some error")
		tnb.mPostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, expectedErr)

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge, nil)
		require.ErrorIs(t, err, expectedErr)
		require.Nil(t, nipost)
		require.Nil(t, nipostInfo)

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostStart()
			require.NotNil(t, event, "wrong event type")
			require.EqualValues(t, shared.ZeroChallenge, event.Challenge)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}

		select {
		case e := <-tnb.eventSub:
			event := e.Event.GetPostComplete()
			require.NotNil(t, event, "wrong event type")
			require.True(t, e.Event.Failure)
			require.Equal(t, "Node failed PoST execution.", e.Event.Help)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}
	})

	t.Run("repeated connection failure", func(t *testing.T) {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)

		tnb := newTestNIPostBuilder(t)
		ctx, cancel := context.WithCancel(context.Background())
		tnb.mPostService.EXPECT().Client(sig.NodeID()).Return(nil, ErrPostClientNotConnected).Times(10)
		tnb.mPostService.EXPECT().Client(sig.NodeID()).DoAndReturn(
			func(types.NodeID) (PostClient, error) {
				cancel()
				return nil, ErrPostClientNotConnected
			})

		nipost, nipostInfo, err := tnb.Proof(ctx, sig.NodeID(), shared.ZeroChallenge, nil)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, nipost)
		require.Nil(t, nipostInfo)

		require.Equal(t, 1, tnb.observedLogs.Len(), "expected 1 log message")
		require.Equal(t, zapcore.WarnLevel, tnb.observedLogs.All()[0].Level)
		require.Equal(t, "post service not connected - waiting for reconnection", tnb.observedLogs.All()[0].Message)
		require.Equal(t, sig.NodeID().String(), tnb.observedLogs.All()[0].ContextMap()["smesherID"])
	})
}

func Test_NIPostBuilder_ResetState(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	postService := NewMockpostService(ctrl)
	mclock := defaultLayerClockMock(ctrl)

	db := localsql.InMemory()

	nb, err := NewNIPostBuilder(
		db,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		nil,
	)
	require.NoError(t, err)

	nipost.AddNIPost(db, sig.NodeID(), &nipost.NIPostState{
		NIPost: &types.NIPost{
			Post: &types.Post{
				Nonce:   1,
				Indices: []byte{1, 2, 3},
				Pow:     1,
			},
			Membership: types.MerkleProof{
				Nodes:     []types.Hash32{types.RandomHash(), types.RandomHash()},
				LeafIndex: 1,
			},
			PostMetadata: &types.PostMetadata{
				Challenge:     types.RandomHash().Bytes(),
				LabelsPerUnit: 1,
			},
		},
		NumUnits: 8,
		VRFNonce: types.VRFPostIndex(1024),
	})

	err = nb.ResetState(sig.NodeID())
	require.NoError(t, err)

	_, err = nipost.NIPost(db, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func Test_NIPostBuilder_WithMocks(t *testing.T) {
	t.Parallel()

	const roundId = "1"
	challenge := types.RandomHash()
	ctrl := gomock.NewController(t)

	poetProvider := defaultPoetServiceMock(t, ctrl, "http://localhost:9999", roundId)
	poetProvider.EXPECT().Proof(gomock.Any(), roundId).Return(&types.PoetProof{}, []types.Hash32{challenge}, nil)

	mclock := defaultLayerClockMock(ctrl)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	postClient := NewMockPostClient(ctrl)
	nonce := types.VRFPostIndex(1)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{}, &types.PostInfo{
		Nonce: &nonce,
	}, nil)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)

	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		nil,
		WithPoetServices(poetProvider),
	)
	require.NoError(t, err)

	nipost, err := nb.BuildNIPost(context.Background(), sig, challenge,
		&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.NoError(t, err)
	require.NotNil(t, nipost)
}

func TestPostSetup(t *testing.T) {
	challenge := types.RandomHash()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	const roundId = "1"

	ctrl := gomock.NewController(t)
	poetProvider := defaultPoetServiceMock(t, ctrl, "http://localhost:9999", roundId)
	poetProvider.EXPECT().Proof(gomock.Any(), roundId).Return(&types.PoetProof{}, []types.Hash32{challenge}, nil)

	mclock := defaultLayerClockMock(ctrl)

	postClient := NewMockPostClient(ctrl)
	nonce := types.VRFPostIndex(1)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{}, &types.PostInfo{
		Nonce: &nonce,
	}, nil)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)

	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		nil,
		WithPoetServices(poetProvider),
	)
	require.NoError(t, err)

	nipost, err := nb.BuildNIPost(context.Background(), sig, challenge,
		&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.NoError(t, err)
	require.NotNil(t, nipost)
}

func TestNIPostBuilder_BuildNIPost(t *testing.T) {
	t.Parallel()
	db := localsql.InMemory()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	require.NoError(t, nipost.AddChallenge(db, sig.NodeID(), &challenge))
	challengeHash := wire.NIPostChallengeToWireV1(&challenge).Hash()

	const roundId = "1"

	ctrl := gomock.NewController(t)
	poetProver := defaultPoetServiceMock(t, ctrl, "http://localhost:9999", roundId)
	poetProver.EXPECT().Proof(gomock.Any(), roundId).AnyTimes().Return(
		&types.PoetProof{}, []types.Hash32{
			challengeHash,
			types.RandomHash(),
			types.RandomHash(),
		}, nil,
	)

	mclock := defaultLayerClockMock(ctrl)

	postClient := NewMockPostClient(ctrl)
	nonce := types.VRFPostIndex(1)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(
		&types.Post{
			Indices: []byte{1, 2, 3},
		}, &types.PostInfo{
			Nonce: &nonce,
		}, nil).Times(1)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil).AnyTimes()

	nb, err := NewNIPostBuilder(
		db,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		nil,
		WithPoetServices(poetProver),
	)
	require.NoError(t, err)

	nipost, err := nb.BuildNIPost(context.Background(), sig, challengeHash, &types.NIPostChallenge{PublishEpoch: 7})
	require.NoError(t, err)
	require.NotNil(t, nipost)

	// fail post exec
	require.NoError(t, nb.ResetState(sig.NodeID()))
	nb, err = NewNIPostBuilder(
		db,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		nil,
		WithPoetServices(poetProver),
	)
	require.NoError(t, err)

	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, errors.New("error"))

	// check that proof ref is not called again
	nipost, err = nb.BuildNIPost(context.Background(), sig, challengeHash, &types.NIPostChallenge{PublishEpoch: 7})

	require.Nil(t, nipost)
	require.Error(t, err)

	// successful post exec
	nb, err = NewNIPostBuilder(
		db,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		nil,
		WithPoetServices(poetProver),
	)
	require.NoError(t, err)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(
		&types.Post{
			Indices: []byte{1, 2, 3},
		}, &types.PostInfo{
			Nonce: &nonce,
		}, nil,
	)

	// check that proof ref is not called again
	nipost, err = nb.BuildNIPost(context.Background(), sig, challengeHash, &types.NIPostChallenge{PublishEpoch: 7})

	require.NoError(t, err)
	require.NotNil(t, nipost)
}

func TestNIPostBuilder_ManyPoETs_SubmittingChallenge_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	challenge := types.RandomHash()
	proof := &types.PoetProof{}

	const roundId = "1"

	ctrl := gomock.NewController(t)
	mclock := defaultLayerClockMock(ctrl)

	poets := make([]PoetService, 0, 2)
	{
		poet := NewMockPoetService(ctrl)
		poet.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(
				ctx context.Context,
				_ time.Time,
				_, _ []byte,
				_ types.EdSignature,
				_ types.NodeID,
			) (*types.PoetRound, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
		poet.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
		poets = append(poets, poet)
	}
	{
		poet := NewMockPoetService(ctrl)
		poet.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&types.PoetRound{ID: roundId}, nil)
		poet.EXPECT().
			Proof(gomock.Any(), roundId).
			Return(proof, []types.Hash32{challenge}, nil)
		poet.EXPECT().Address().AnyTimes().Return("http://localhost:9998")
		poets = append(poets, poet)
	}

	poetCfg := PoetConfig{
		PhaseShift: layerDuration * layersPerEpoch / 2,
	}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	postClient := NewMockPostClient(ctrl)
	nonce := types.VRFPostIndex(1)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{}, &types.PostInfo{
		Nonce: &nonce,
	}, nil)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)

	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		postService,
		zaptest.NewLogger(t),
		poetCfg,
		mclock,
		nil,
		WithPoetServices(poets...),
	)
	require.NoError(t, err)

	// Act
	nipost, err := nb.BuildNIPost(context.Background(), sig, challenge,
		&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.NoError(t, err)

	// Verify
	ref, _ := proof.Ref()
	require.Equal(t, ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_ManyPoETs_AllFinished(t *testing.T) {
	t.Parallel()

	const roundId1 = "1"

	// Arrange
	challenge := types.RandomHash()
	proofWorse := &types.PoetProof{LeafCount: 111}
	proofBetter := &types.PoetProof{LeafCount: 999}

	ctrl := gomock.NewController(t)
	mclock := defaultLayerClockMock(ctrl)

	poets := make([]PoetService, 0, 2)
	{
		poet := NewMockPoetService(ctrl)
		poet.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&types.PoetRound{ID: roundId1}, nil)
		poet.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
		poet.EXPECT().Proof(gomock.Any(), roundId1).Return(proofWorse, []types.Hash32{challenge}, nil)
		poets = append(poets, poet)
	}
	{
		poet := defaultPoetServiceMock(t, ctrl, "http://localhost:9998", roundId1)
		poet.EXPECT().Proof(gomock.Any(), roundId1).Return(proofBetter, []types.Hash32{challenge}, nil)
		poets = append(poets, poet)
	}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	postClient := NewMockPostClient(ctrl)
	nonce := types.VRFPostIndex(1)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{}, &types.PostInfo{
		Nonce: &nonce,
	}, nil)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)

	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		nil,
		WithPoetServices(poets...),
	)
	require.NoError(t, err)

	// Act
	nipost, err := nb.BuildNIPost(context.Background(), sig, challenge,
		&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.NoError(t, err)

	// Verify
	ref, _ := proofBetter.Ref()
	require.Equal(t, ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPSTBuilder_PoetUnstable(t *testing.T) {
	t.Parallel()
	challenge := types.RandomHash()
	poetCfg := PoetConfig{
		PhaseShift: layerDuration,
	}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	const roundId = "1"

	t.Run("Submit fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := NewMockPoetService(ctrl)
		poetProver.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), sig.NodeID()).
			Return(nil, errors.New("test"))
		poetProver.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			nil,
			WithPoetServices(poetProver),
		)
		require.NoError(t, err)

		nipst, err := nb.BuildNIPost(
			context.Background(),
			sig,
			challenge,
			&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2},
		)
		poetErr := &PoetSvcUnstableError{}
		require.ErrorAs(t, err, &poetErr)
		require.Nil(t, nipst)
	})
	t.Run("Submit hangs, no registrations submitted", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := NewMockPoetService(ctrl)

		poetProver.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), sig.NodeID()).
			DoAndReturn(func(
				ctx context.Context,
				_ time.Time,
				_, _ []byte,
				_ types.EdSignature,
				_ types.NodeID,
			) (*types.PoetRound, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
		poetProver.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			nil,
			WithPoetServices(poetProver),
		)
		require.NoError(t, err)

		nipst, err := nb.BuildNIPost(
			context.Background(),
			sig,
			challenge,
			&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2},
		)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.Nil(t, nipst)
	})
	t.Run("GetProof fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := defaultPoetServiceMock(t, ctrl, "http://localhost:9999", roundId)
		poetProver.EXPECT().Proof(gomock.Any(), roundId).Return(nil, nil, errors.New("failed"))
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			nil,
			WithPoetServices(poetProver),
		)
		require.NoError(t, err)

		nipst, err := nb.BuildNIPost(context.Background(), sig, challenge,
			&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
	t.Run("Challenge is not included in proof members", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := defaultPoetServiceMock(t, ctrl, "http://localhost:9999", roundId)
		poetProver.EXPECT().
			Proof(gomock.Any(), roundId).
			Return(&types.PoetProof{}, []types.Hash32{}, nil)
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			nil,
			WithPoetServices(poetProver),
		)
		require.NoError(t, err)

		nipst, err := nb.BuildNIPost(context.Background(), sig, challenge,
			&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
}

// TestNIPoSTBuilder_PoETConfigChange checks if
// it properly detects added/deleted PoET services and re-registers if needed.
func TestNIPoSTBuilder_PoETConfigChange(t *testing.T) {
	t.Parallel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	challengeHash := wire.NIPostChallengeToWireV1(&challenge).Hash()

	const (
		poetProverAddr  = "http://localhost:9999"
		poetProverAddr2 = "http://localhost:9988"
		poetProverAddr3 = "http://localhost:9977"

		roundId = "1"
	)

	t.Run("1 poet deleted BEFORE round started -> re-submit failed registration, continue with submitted registrations",
		func(t *testing.T) {
			db := localsql.InMemory()
			ctrl := gomock.NewController(t)

			poet := NewMockPoetService(ctrl)
			poet.EXPECT().Address().Return(poetProverAddr).AnyTimes()

			poet2 := NewMockPoetService(ctrl)
			poet2.EXPECT().
				Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&types.PoetRound{ID: roundId, End: time.Now().Add(30 * time.Second)}, nil)
			poet2.EXPECT().Address().Return(poetProverAddr3).AnyTimes()

			// successfully registered to 2 poets
			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr,
				RoundID:       roundId,
				RoundEnd:      time.Now().Add(1 * time.Second),
			})
			require.NoError(t, err)

			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr2,
				RoundID:       roundId,
				RoundEnd:      time.Now().Add(1 * time.Second),
			})

			// 1 registration failed -> will try to re-register
			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr3,
			})

			nb, err := NewNIPostBuilder(
				db,
				nil,
				zaptest.NewLogger(t),
				PoetConfig{},
				nil,
				nil,
				WithPoetServices(poet, poet2), // 1 poet was removed
			)
			require.NoError(t, err)

			existingRegistrations, err := nb.submitPoetChallenges(
				context.Background(),
				sig,
				time.Time{},
				time.Now().Add(10*time.Second),
				time.Now().Add(5*time.Second),
				challengeHash.Bytes())

			require.NoError(t, err)
			require.Len(t, existingRegistrations, 2)

			regsMap := make(map[string]nipost.PoETRegistration)
			for _, reg := range existingRegistrations {
				regsMap[reg.Address] = reg
			}

			_, ok := regsMap[poetProverAddr]
			require.True(t, ok)

			newReg, ok := regsMap[poetProverAddr3]
			require.True(t, ok)
			require.Equal(t, roundId, newReg.RoundID)
			require.NotEmpty(t, newReg.RoundEnd)
		})

	t.Run("1 poet added BEFORE round started -> register to missing poet", func(t *testing.T) {
		db := localsql.InMemory()
		ctrl := gomock.NewController(t)

		poetProver := NewMockPoetService(ctrl)
		poetProver.EXPECT().Address().Return(poetProverAddr).AnyTimes()

		addedPoetProver := NewMockPoetService(ctrl)
		addedPoetProver.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&types.PoetRound{}, nil)
		addedPoetProver.EXPECT().Address().Return(poetProverAddr2).AnyTimes()

		// successfully registered to 1 poet
		err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
			NodeId:        sig.NodeID(),
			ChallengeHash: challengeHash,
			Address:       poetProverAddr,
			RoundID:       "1",
			RoundEnd:      time.Now().Add(1 * time.Second),
		})
		require.NoError(t, err)

		// successful post exec
		nb, err := NewNIPostBuilder(
			db,
			nil,
			zaptest.NewLogger(t),
			PoetConfig{},
			nil,
			nil,
			WithPoetServices(poetProver, addedPoetProver), // add both poet provers
		)
		require.NoError(t, err)

		existingRegistrations, err := nb.submitPoetChallenges(
			context.Background(),
			sig,
			time.Time{},
			time.Now().Add(10*time.Second),
			time.Now().Add(5*time.Second),
			challengeHash.Bytes())

		require.NoError(t, err)
		require.Len(t, existingRegistrations, 2)
		require.Equal(t, poetProverAddr, existingRegistrations[0].Address)
		require.Equal(t, poetProverAddr2, existingRegistrations[1].Address)
	})

	t.Run("completely changed poet service BEFORE round started -> register new poet", func(t *testing.T) {
		db := localsql.InMemory()
		ctrl := gomock.NewController(t)

		addedPoetProver := NewMockPoetService(ctrl)
		addedPoetProver.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&types.PoetRound{}, nil)
		addedPoetProver.EXPECT().Address().Return(poetProverAddr2).AnyTimes()

		// successfully registered to removed poet
		err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
			NodeId:        sig.NodeID(),
			ChallengeHash: challengeHash,
			Address:       poetProverAddr,
			RoundID:       "1",
			RoundEnd:      time.Now().Add(1 * time.Second),
		})
		require.NoError(t, err)

		nb, err := NewNIPostBuilder(
			db,
			nil,
			zaptest.NewLogger(t),
			PoetConfig{},
			nil,
			nil,
			WithPoetServices(addedPoetProver), // add new poet
		)
		require.NoError(t, err)

		existingRegistrations, err := nb.submitPoetChallenges(
			context.Background(),
			sig,
			time.Time{},
			time.Now().Add(5*time.Second),
			time.Now().Add(10*time.Second),
			challengeHash.Bytes())

		require.NoError(t, err)
		require.Len(t, existingRegistrations, 1)
		require.Equal(t, poetProverAddr2, existingRegistrations[0].Address)
	})

	t.Run("1 poet added AFTER round started -> too late to register to added poet",
		func(t *testing.T) {
			db := localsql.InMemory()
			ctrl := gomock.NewController(t)

			poetProver := NewMockPoetService(ctrl)
			poetProver.EXPECT().Address().Return(poetProverAddr).AnyTimes()

			addedPoetProver := NewMockPoetService(ctrl)
			addedPoetProver.EXPECT().Address().Return(poetProverAddr2).AnyTimes()

			// successfully registered to 1 poet
			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr,
				RoundID:       "1",
				RoundEnd:      time.Now().Add(1 * time.Second),
			})
			require.NoError(t, err)

			// successful post exec
			nb, err := NewNIPostBuilder(
				db,
				nil,
				zaptest.NewLogger(t),
				PoetConfig{},
				nil,
				nil,
				WithPoetServices(poetProver, addedPoetProver),
			)
			require.NoError(t, err)

			existingRegistrations, err := nb.submitPoetChallenges(
				context.Background(),
				sig,
				time.Time{},
				time.Now().Add(-5*time.Second), // poet round started
				time.Now().Add(10*time.Second),
				challengeHash.Bytes())

			require.NoError(t, err)
			require.Len(t, existingRegistrations, 1)
			require.Equal(t, poetProverAddr, existingRegistrations[0].Address)
		})

	t.Run("1 failed registration exists, poet added AFTER round started -> too late to submit challenges",
		func(t *testing.T) {
			db := localsql.InMemory()
			ctrl := gomock.NewController(t)

			poetProver := NewMockPoetService(ctrl)
			poetProver.EXPECT().Address().Return(poetProverAddr).AnyTimes()

			addedPoetProver := NewMockPoetService(ctrl)
			addedPoetProver.EXPECT().Address().Return(poetProverAddr2).AnyTimes()

			// failed registration
			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr,
			})
			require.NoError(t, err)

			// successful post exec
			nb, err := NewNIPostBuilder(
				db,
				nil,
				zaptest.NewLogger(t),
				PoetConfig{},
				nil,
				nil,
				WithPoetServices(poetProver, addedPoetProver),
			)
			require.NoError(t, err)

			existingRegistrations, err := nb.submitPoetChallenges(
				context.Background(),
				sig,
				time.Time{},
				time.Now().Add(-5*time.Second), // poet round started
				time.Now().Add(10*time.Second),
				challengeHash.Bytes())

			poetErr := &PoetRegistrationMismatchError{}
			require.ErrorAs(t, err, &poetErr)
			require.ElementsMatch(t, poetErr.configuredPoets, []string{poetProverAddr, poetProverAddr2})
			require.Empty(t, poetErr.registrations)
			require.Empty(t, existingRegistrations)
		})

	t.Run("1 poet removed AFTER round started -> too late to register to added poet",
		func(t *testing.T) {
			db := localsql.InMemory()
			ctrl := gomock.NewController(t)

			poetProver := NewMockPoetService(ctrl)
			poetProver.EXPECT().Address().Return(poetProverAddr).AnyTimes()

			addedPoetProver := NewMockPoetService(ctrl)
			addedPoetProver.EXPECT().Address().Return(poetProverAddr2).AnyTimes()

			// successfully registered to 2 poets
			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr,
				RoundID:       "1",
				RoundEnd:      time.Now().Add(1 * time.Second),
			})
			require.NoError(t, err)

			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr2,
				RoundID:       "1",
				RoundEnd:      time.Now().Add(1 * time.Second),
			})

			nb, err := NewNIPostBuilder(
				db,
				nil,
				zaptest.NewLogger(t),
				PoetConfig{},
				nil,
				nil,
				WithPoetServices(poetProver),
			)
			require.NoError(t, err)

			existingRegistrations, err := nb.submitPoetChallenges(
				context.Background(),
				sig,
				time.Time{},
				time.Now().Add(-5*time.Second), // poet round started
				time.Now().Add(10*time.Second),
				challengeHash.Bytes())

			require.NoError(t, err)
			require.Len(t, existingRegistrations, 1)
			require.Equal(t, poetProverAddr, existingRegistrations[0].Address)
		})

	t.Run("completely changed poet service AFTER round started -> fail, too late to register again",
		func(t *testing.T) {
			db := localsql.InMemory()
			ctrl := gomock.NewController(t)

			poetProver := NewMockPoetService(ctrl)
			poetProver.EXPECT().Address().Return(poetProverAddr).AnyTimes()

			// successfully registered to removed poet
			err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
				NodeId:        sig.NodeID(),
				ChallengeHash: challengeHash,
				Address:       poetProverAddr2,
				RoundID:       "1",
				RoundEnd:      time.Now().Add(1 * time.Second),
			})
			require.NoError(t, err)

			logger := zaptest.NewLogger(t)

			nb, err := NewNIPostBuilder(
				db,
				nil,
				logger,
				PoetConfig{},
				nil,
				nil,
				WithPoetServices(poetProver),
			)
			require.NoError(t, err)

			_, err = nb.submitPoetChallenges(
				context.Background(),
				sig,
				time.Time{},
				time.Now().Add(-5*time.Second), // poet round started
				time.Now().Add(10*time.Second),
				challengeHash.Bytes(),
			)
			poetErr := &PoetRegistrationMismatchError{}
			require.ErrorAs(t, err, &poetErr)
			require.ElementsMatch(t, poetErr.configuredPoets, []string{poetProverAddr})
			require.ElementsMatch(t, poetErr.registrations, []string{poetProverAddr2})
		})
}

// TestNIPoSTBuilder_StaleChallenge checks if
// it properly detects that the challenge is stale and the poet round has already started.
func TestNIPoSTBuilder_StaleChallenge(t *testing.T) {
	t.Parallel()

	currLayer := types.EpochID(10).FirstLayer()
	genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	const poetAddr = "http://localhost:9999"

	// Act & Verify
	t.Run("no requests, poet round started", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetService(ctrl)
		poetProver.EXPECT().Address().Return(poetAddr).AnyTimes()
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			},
		).AnyTimes()
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			postService,
			zaptest.NewLogger(t),
			PoetConfig{},
			mclock,
			nil,
			WithPoetServices(poetProver),
		)
		require.NoError(t, err)

		nipost, err := nb.BuildNIPost(context.Background(), sig, types.RandomHash(),
			&types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch()})
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "poet round has already started")
		require.Nil(t, nipost)
	})
	t.Run("no response before deadline", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetService(ctrl)
		poetProver.EXPECT().Address().Return(poetAddr).AnyTimes()
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			}).AnyTimes()
		postService := NewMockpostService(ctrl)

		db := localsql.InMemory()
		nb, err := NewNIPostBuilder(
			db,
			postService,
			zaptest.NewLogger(t),
			PoetConfig{},
			mclock,
			nil,
			WithPoetServices(poetProver),
		)
		require.NoError(t, err)

		challenge := &types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch() - 1}
		challengeHash := wire.NIPostChallengeToWireV1(challenge).Hash()
		err = nipost.AddChallenge(db, sig.NodeID(), challenge)
		require.NoError(t, err)

		// successfully registered to at least one poet
		err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
			NodeId:        sig.NodeID(),
			ChallengeHash: challengeHash,
			Address:       poetAddr,
			RoundID:       "1",
			RoundEnd:      time.Now().Add(10 * time.Second),
		})
		require.NoError(t, err)

		nipost, err := nb.BuildNIPost(context.Background(), sig, challengeHash, challenge)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "poet proof for pub epoch")
		require.Nil(t, nipost)
	})
	t.Run("too late for proof generation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetService(ctrl)
		poetProver.EXPECT().Address().Return(poetAddr).AnyTimes()
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			}).AnyTimes()
		postService := NewMockpostService(ctrl)

		db := localsql.InMemory()
		nb, err := NewNIPostBuilder(
			db,
			postService,
			zaptest.NewLogger(t),
			PoetConfig{},
			mclock,
			nil,
			WithPoetServices(poetProver),
		)
		require.NoError(t, err)

		challenge := &types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch() - 1}
		challengeHash := wire.NIPostChallengeToWireV1(challenge).Hash()
		err = nipost.AddChallenge(db, sig.NodeID(), challenge)
		require.NoError(t, err)

		// successfully registered to at least one poet
		err = nipost.AddPoetRegistration(db, nipost.PoETRegistration{
			NodeId:        sig.NodeID(),
			ChallengeHash: challengeHash,
			Address:       poetAddr,
			RoundID:       "1",
			RoundEnd:      time.Now().Add(10 * time.Second),
		})
		require.NoError(t, err)

		// received a proof from poet
		err = nipost.UpdatePoetProofRef(db, sig.NodeID(), [32]byte{1, 2, 3}, &types.MerkleProof{})
		require.NoError(t, err)

		nipost, err := nb.BuildNIPost(context.Background(), sig, challengeHash, challenge)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "deadline to publish ATX for pub epoch")
		require.Nil(t, nipost)
	})
}

// Test if the NIPoSTBuilder continues after being interrupted just after
// a challenge has been submitted to poet.
func TestNIPoSTBuilder_Continues_After_Interrupted(t *testing.T) {
	t.Parallel()

	// Arrange
	challenge := types.RandomHash()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	proof := &types.PoetProof{LeafCount: 777}

	ctrl := gomock.NewController(t)
	mclock := defaultLayerClockMock(ctrl)

	buildCtx, cancel := context.WithCancel(context.Background())

	poet := NewMockPoetService(ctrl)
	poet.EXPECT().
		Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(
			_ context.Context,
			_ time.Time,
			_, _ []byte,
			_ types.EdSignature,
			_ types.NodeID,
		) (*types.PoetRound, error) {
			cancel()
			return &types.PoetRound{}, context.Canceled
		})

	poet.EXPECT().Address().AnyTimes().Return("http://localhost:9999")

	poetCfg := PoetConfig{
		PhaseShift: layerDuration * layersPerEpoch / 2,
	}

	postClient := NewMockPostClient(ctrl)
	nonce := types.VRFPostIndex(1)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{}, &types.PostInfo{
		Nonce: &nonce,
	}, nil)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)

	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		postService,
		zaptest.NewLogger(t),
		poetCfg,
		mclock,
		nil,
		WithPoetServices(poet),
	)
	require.NoError(t, err)

	// Act
	nipost, err := nb.BuildNIPost(buildCtx, sig, challenge, &types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, nipost)

	// allow to submit in the second try
	const roundId = "1"

	poet.EXPECT().
		Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&types.PoetRound{ID: roundId}, nil)

	poet.EXPECT().Proof(gomock.Any(), roundId).Return(proof, []types.Hash32{challenge}, nil)
	poet.EXPECT().Address().AnyTimes().Return("http://localhost:9999")

	nipost, err = nb.BuildNIPost(context.Background(), sig, challenge,
		&types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.NoError(t, err)

	// Verify
	ref, _ := proof.Ref()
	require.Equal(t, ref[:], nipost.PostMetadata.Challenge)
}

func TestConstructingMerkleProof(t *testing.T) {
	challenge := types.RandomHash()

	t.Run("members empty", func(t *testing.T) {
		_, err := constructMerkleProof(challenge, []types.Hash32{})
		require.Error(t, err)
	})
	t.Run("not a member", func(t *testing.T) {
		_, err := constructMerkleProof(challenge, []types.Hash32{{}, {}})
		require.Error(t, err)
	})

	t.Run("is odd member", func(t *testing.T) {
		members := []types.Hash32{
			challenge,
			types.RandomHash(),
		}
		proof, err := constructMerkleProof(challenge, members)
		require.NoError(t, err)

		root, err := calcRoot(members)
		require.NoError(t, err)

		err = validateMerkleProof(challenge[:], proof, root)
		require.NoError(t, err)
	})
	t.Run("is even member", func(t *testing.T) {
		members := []types.Hash32{
			types.RandomHash(),
			challenge,
		}
		proof, err := constructMerkleProof(challenge, members)
		require.NoError(t, err)

		root, err := calcRoot(members)
		require.NoError(t, err)

		err = validateMerkleProof(challenge[:], proof, root)
		require.NoError(t, err)
	})
}

func TestNIPostBuilder_Mainnet_Poet_Workaround(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name  string
		from  string
		to    string
		epoch types.EpochID
	}{
		// no mitigation needed at the moment
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			challenge := types.RandomHash()
			ctrl := gomock.NewController(t)
			poets := make([]PoetService, 0, 2)
			{
				poetProvider := NewMockPoetService(ctrl)
				poetProvider.EXPECT().Address().Return(tc.from)

				// PoET succeeds to submit
				poetProvider.EXPECT().
					Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&types.PoetRound{}, nil)

				// proof is fetched from PoET
				poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProof{}, []types.Hash32{challenge}, nil)
				poets = append(poets, poetProvider)
			}

			{
				// PoET fails submission
				poetProvider := NewMockPoetService(ctrl)
				poetProvider.EXPECT().Address().Return(tc.to)

				// proof is still fetched from PoET
				poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProof{}, []types.Hash32{challenge}, nil)

				poets = append(poets, poetProvider)
			}

			mclock := NewMocklayerClock(ctrl)
			genesis := time.Now().Add(-time.Duration(1) * layerDuration)
			mclock.EXPECT().LayerToTime(gomock.Any()).AnyTimes().DoAndReturn(
				func(got types.LayerID) time.Time {
					return genesis.Add(layerDuration * time.Duration(got))
				},
			)

			poetCfg := PoetConfig{
				PhaseShift: layerDuration * layersPerEpoch / 2,
			}

			sig, err := signing.NewEdSigner()
			require.NoError(t, err)

			postClient := NewMockPostClient(ctrl)
			postClient.EXPECT().Proof(gomock.Any(), gomock.Any())
			postService := NewMockpostService(ctrl)
			postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)

			nb, err := NewNIPostBuilder(
				localsql.InMemory(),
				postService,
				zaptest.NewLogger(t),
				poetCfg,
				mclock,
				nil,
				WithPoetServices(poets...),
			)
			require.NoError(t, err)

			nipost, err := nb.BuildNIPost(context.Background(), sig, challenge,
				&types.NIPostChallenge{PublishEpoch: tc.epoch})
			require.NoError(t, err)
			require.NotNil(t, nipost)
		})
	}
}

func TestRandomDurationInRange(t *testing.T) {
	t.Parallel()
	test := func(min, max time.Duration) {
		for range 100 {
			waitTime := randomDurationInRange(min, max)
			require.LessOrEqual(t, waitTime, max)
			require.GreaterOrEqual(t, waitTime, min)
		}
	}
	t.Run("min = 0", func(t *testing.T) {
		t.Parallel()
		test(0, 7*time.Second)
	})
	t.Run("min != 0", func(t *testing.T) {
		t.Parallel()
		test(5*time.Second, 7*time.Second)
	})
}

func TestCalculatingGetProofWaitTime(t *testing.T) {
	t.Parallel()
	t.Run("past round end", func(t *testing.T) {
		t.Parallel()
		deadline := proofDeadline(time.Now().Add(-time.Hour), time.Hour*12)
		require.Less(t, time.Until(deadline), time.Duration(0))
	})
	t.Run("before round end", func(t *testing.T) {
		t.Parallel()
		cycleGap := 12 * time.Hour
		deadline := proofDeadline(time.Now().Add(time.Hour), cycleGap)

		require.Greater(t, time.Until(deadline), time.Hour+time.Duration(float64(cycleGap)*minPoetGetProofJitter/100))
		require.LessOrEqual(
			t,
			time.Until(deadline),
			time.Hour+time.Duration(float64(cycleGap)*maxPoetGetProofJitter/100),
		)
	})
}

func TestNIPostBuilder_Close(t *testing.T) {
	ctrl := gomock.NewController(t)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	const roundId = "1"

	poet := defaultPoetServiceMock(t, ctrl, "http://localhost:9999", roundId)
	poet.EXPECT().Proof(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, _ string) (*types.PoetProofMessage, []types.Hash32, error) {
			return nil, nil, ctx.Err()
		})
	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		NewMockpostService(ctrl),
		zaptest.NewLogger(t),
		PoetConfig{},
		defaultLayerClockMock(ctrl),
		nil,
		WithPoetServices(poet),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	challenge := types.RandomHash()

	_, err = nb.BuildNIPost(ctx, sig, challenge, &types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2})
	require.Error(t, err)
}

func TestNIPostBuilderProof_WithBadInitialPost(t *testing.T) {
	ctrl := gomock.NewController(t)
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	const roundId = "1"
	poet := defaultPoetServiceMock(t, ctrl, "http://localhost:9999", roundId)
	poet.EXPECT().Proof(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, _ string) (*types.PoetProofMessage, []types.Hash32, error) {
			return nil, nil, ctx.Err()
		})
	validator := NewMocknipostValidator(ctrl)
	validator.EXPECT().PostV2(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(errors.New("some error"))

	postClient := NewMockPostClient(ctrl)
	postClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{}, nil)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)
	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		defaultLayerClockMock(ctrl),
		validator,
		WithPoetServices(poet),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	challenge := types.RandomHash()
	_, _, err = nb.Proof(ctx, sig.NodeID(), challenge[:], &types.NIPostChallenge{InitialPost: &types.Post{}})
	require.ErrorIs(t, err, ErrInvalidInitialPost)
}
