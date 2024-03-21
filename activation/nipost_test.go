package activation

import (
	"context"
	"errors"
	"fmt"
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

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/certifier"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

func defaultPoetServiceMock(ctrl *gomock.Controller, address string) *MockPoetClient {
	poetClient := NewMockPoetClient(ctrl)
	poetClient.EXPECT().
		Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes().
		Return(&types.PoetRound{}, nil)
	poetClient.EXPECT().PowParams(gomock.Any()).AnyTimes().Return(&PoetPowParams{}, nil)
	poetClient.EXPECT().Address().AnyTimes().Return(address).AnyTimes()
	return poetClient
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

	mDb          *localsql.Database
	mLogger      *zap.Logger
	mPoetDb      *MockpoetDbAPI
	mClock       *MocklayerClock
	mPostService *MockpostService
	mPostClient  *MockPostClient
}

func newTestNIPostBuilder(tb testing.TB) *testNIPostBuilder {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zap.New(observer)

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
	}

	nb, err := NewNIPostBuilder(
		tnb.mDb,
		tnb.mPoetDb,
		tnb.mPostService,
		tnb.mLogger,
		PoetConfig{},
		tnb.mClock,
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

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge)
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
			require.Equal(t, false, e.Event.Failure)
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

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge)
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
			require.Equal(t, true, e.Event.Failure)
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

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge)
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
			require.Equal(t, false, e.Event.Failure)
			require.Equal(t, "Node finished PoST execution using PoET challenge.", e.Event.Help)
		case <-time.After(5 * time.Second):
			require.Fail(t, "timeout waiting for event")
		}
	})

	t.Run("connect, disconnect, then cancel before reconnect", func(t *testing.T) {
		// post client connects, starts post, disconnects in between and proofing is canceled before reconnection
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
				require.Equal(t, true, e.Event.Failure)
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

		nipost, nipostInfo, err := tnb.Proof(ctx, sig.NodeID(), shared.ZeroChallenge)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, nipost)
		require.Nil(t, nipostInfo)

		require.Nil(t, eg.Wait())
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

		nipost, nipostInfo, err := tnb.Proof(context.Background(), sig.NodeID(), shared.ZeroChallenge)
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
			require.Equal(t, true, e.Event.Failure)
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

		nipost, nipostInfo, err := tnb.Proof(ctx, sig.NodeID(), shared.ZeroChallenge)
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
	poetDb := NewMockpoetDbAPI(ctrl)
	postService := NewMockpostService(ctrl)
	mclock := defaultLayerClockMock(ctrl)

	db := localsql.InMemory()

	nb, err := NewNIPostBuilder(
		db,
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
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

	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	ctrl := gomock.NewController(t)

	poetProvider := defaultPoetServiceMock(ctrl, "http://localhost:9999")
	poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
		PoetProof: types.PoetProof{},
	}, []types.Member{types.Member(challenge.Hash())}, nil)

	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
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
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		WithPoetClients(poetProvider),
	)
	require.NoError(t, err)

	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().GetCertificate(poetProvider.Address()).Return(&certifier.PoetCert{Data: []byte("cert")})

	nipost, err := nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
	require.NoError(t, err)
	require.NotNil(t, nipost)
}

func TestPostSetup(t *testing.T) {
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	poetProvider := defaultPoetServiceMock(ctrl, "http://localhost:9999")
	poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
		PoetProof: types.PoetProof{},
	}, []types.Member{types.Member(challenge.Hash())}, nil)

	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
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
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		WithPoetClients(poetProvider),
	)
	require.NoError(t, err)

	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().GetCertificate(poetProvider.Address()).Return(&certifier.PoetCert{Data: []byte("cert")})

	nipost, err := nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
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
	challenge2 := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
		Sequence:     1,
	}
	challenge3 := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
		Sequence:     2,
	}

	ctrl := gomock.NewController(t)
	poetProver := defaultPoetServiceMock(ctrl, "http://localhost:9999")
	poetProver.EXPECT().Proof(gomock.Any(), "").AnyTimes().Return(
		&types.PoetProofMessage{
			PoetProof: types.PoetProof{},
		}, []types.Member{
			types.Member(challenge.Hash()),
			types.Member(challenge2.Hash()),
			types.Member(challenge3.Hash()),
		}, nil,
	)

	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
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
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		WithPoetClients(poetProver),
	)
	require.NoError(t, err)

	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().GetCertificate(poetProver.Address()).AnyTimes().Return(&certifier.PoetCert{Data: []byte("cert")})
	nipost, err := nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
	require.NoError(t, err)
	require.NotNil(t, nipost)

	poetDb = NewMockpoetDbAPI(ctrl)

	// fail post exec
	require.NoError(t, nb.ResetState(sig.NodeID()))
	nb, err = NewNIPostBuilder(
		db,
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		WithPoetClients(poetProver),
	)
	require.NoError(t, err)

	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(nil, nil, fmt.Errorf("error"))

	// check that proof ref is not called again
	nipost, err = nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
	require.Nil(t, nipost)
	require.Error(t, err)

	// successful post exec
	nb, err = NewNIPostBuilder(
		db,
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		WithPoetClients(poetProver),
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
	nipost, err = nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
	require.NoError(t, err)
	require.NotNil(t, nipost)
}

func TestNIPostBuilder_ManyPoETs_SubmittingChallenge_DeadlineReached(t *testing.T) {
	t.Parallel()
	// Arrange
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 1,
	}

	proof := &types.PoetProofMessage{PoetProof: types.PoetProof{}}

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(ctrl)

	poets := make([]PoetClient, 0, 2)
	{
		poet := NewMockPoetClient(ctrl)
		poet.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(
				ctx context.Context,
				_ time.Time,
				_, _ []byte,
				_ types.EdSignature,
				_ types.NodeID,
				_ PoetAuth,
			) (*types.PoetRound, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
		poet.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
		poets = append(poets, poet)
	}
	{
		poet := NewMockPoetClient(ctrl)
		poet.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&types.PoetRound{}, nil)
		poet.EXPECT().
			Proof(gomock.Any(), gomock.Any()).
			Return(proof, []types.Member{types.Member(challenge.Hash())}, nil)
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
		poetDb,
		postService,
		zaptest.NewLogger(t),
		poetCfg,
		mclock,
		WithPoetClients(poets...),
	)
	require.NoError(t, err)

	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().GetCertificate(poets[0].Address()).Return(&certifier.PoetCert{Data: []byte("cert")})
	mCertifier.EXPECT().GetCertificate(poets[1].Address()).Return(&certifier.PoetCert{Data: []byte("cert")})

	// Act
	nipost, err := nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
	require.NoError(t, err)

	// Verify
	ref, _ := proof.Ref()
	require.Equal(t, ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPostBuilder_ManyPoETs_AllFinished(t *testing.T) {
	t.Parallel()

	// Arrange
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	proofWorse := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			LeafCount: 111,
		},
	}
	proofBetter := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			LeafCount: 999,
		},
	}

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Times(2).Return(nil)
	mclock := defaultLayerClockMock(ctrl)

	poets := make([]PoetClient, 0, 2)
	{
		poet := NewMockPoetClient(ctrl)
		poet.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(&types.PoetRound{}, nil)
		poet.EXPECT().Address().AnyTimes().Return("http://localhost:9999").AnyTimes()
		poet.EXPECT().Proof(gomock.Any(), "").Return(proofWorse, []types.Member{types.Member(challenge.Hash())}, nil)
		poets = append(poets, poet)
	}
	{
		poet := defaultPoetServiceMock(ctrl, "http://localhost:9998")
		poet.EXPECT().Proof(gomock.Any(), "").Return(proofBetter, []types.Member{types.Member(challenge.Hash())}, nil)
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
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{},
		mclock,
		WithPoetClients(poets...),
	)
	require.NoError(t, err)

	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().GetCertificate(poets[0].Address()).Return(&certifier.PoetCert{Data: []byte("cert")})
	// No certs - fallback to PoW
	mCertifier.EXPECT().GetCertificate(poets[1].Address()).Return(nil)

	// Act
	nipost, err := nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
	require.NoError(t, err)

	// Verify
	ref, _ := proofBetter.Ref()
	require.Equal(t, ref[:], nipost.PostMetadata.Challenge)
}

func TestNIPSTBuilder_PoetUnstable(t *testing.T) {
	t.Parallel()
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 1,
	}
	poetCfg := PoetConfig{
		PhaseShift: layerDuration,
	}
	cert := &certifier.PoetCert{Data: []byte("cert")}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	t.Run("Submit fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := NewMockPoetClient(ctrl)
		poetProver.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), sig.NodeID(), PoetAuth{PoetCert: cert}).
			Return(nil, errors.New("test"))
		poetProver.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			poetDb,
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			WithPoetClients(poetProver),
		)
		require.NoError(t, err)

		certifier := NewMockcertifierService(ctrl)
		certifier.EXPECT().GetCertificate("http://localhost:9999").Return(cert)

		nipst, err := nb.BuildNIPost(context.Background(), sig, &challenge, certifier)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("Submit hangs", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := NewMockPoetClient(ctrl)
		poetProver.EXPECT().
			Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), sig.NodeID(), PoetAuth{PoetCert: cert}).
			DoAndReturn(func(
				ctx context.Context,
				_ time.Time,
				_, _ []byte,
				_ types.EdSignature,
				_ types.NodeID,
				_ PoetAuth,
			) (*types.PoetRound, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			})
		poetProver.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			poetDb,
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			WithPoetClients(poetProver),
		)
		require.NoError(t, err)

		certifier := NewMockcertifierService(ctrl)
		certifier.EXPECT().GetCertificate("http://localhost:9999").Return(cert)

		nipst, err := nb.BuildNIPost(context.Background(), sig, &challenge, certifier)
		require.ErrorIs(t, err, ErrPoetServiceUnstable)
		require.Nil(t, nipst)
	})
	t.Run("GetProof fails", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := defaultPoetServiceMock(ctrl, "http://localhost:9999")
		poetProver.EXPECT().Proof(gomock.Any(), "").Return(nil, nil, errors.New("failed"))
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			poetDb,
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			WithPoetClients(poetProver),
		)
		require.NoError(t, err)

		certifier := NewMockcertifierService(ctrl)
		certifier.EXPECT().GetCertificate("http://localhost:9999").Return(cert)

		nipst, err := nb.BuildNIPost(context.Background(), sig, &challenge, certifier)
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
	t.Run("Challenge is not included in proof members", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
		mclock := defaultLayerClockMock(ctrl)
		poetProver := defaultPoetServiceMock(ctrl, "http://localhost:9999")
		poetProver.EXPECT().
			Proof(gomock.Any(), "").
			Return(&types.PoetProofMessage{PoetProof: types.PoetProof{}}, []types.Member{}, nil)
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			poetDb,
			postService,
			zaptest.NewLogger(t),
			poetCfg,
			mclock,
			WithPoetClients(poetProver),
		)
		require.NoError(t, err)

		certifier := NewMockcertifierService(ctrl)
		certifier.EXPECT().GetCertificate("http://localhost:9999").Return(cert)

		nipst, err := nb.BuildNIPost(context.Background(), sig, &challenge, certifier)
		require.ErrorIs(t, err, ErrPoetProofNotReceived)
		require.Nil(t, nipst)
	})
}

func TestNIPostBuilder_RecertifyPoet(t *testing.T) {
	t.Parallel()
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 1,
	}

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(ctrl)
	poetProver := NewMockPoetClient(ctrl)
	poetProver.EXPECT().Address().AnyTimes().Return("http://localhost:9999")
	postClient := NewMockPostClient(ctrl)
	nonce := types.VRFPostIndex(1)
	postClient.EXPECT().Proof(gomock.Any(), gomock.Any()).Return(&types.Post{
		Indices: []byte{1, 2, 3},
	}, &types.PostInfo{
		Nonce: &nonce,
	}, nil)
	postService := NewMockpostService(ctrl)
	postService.EXPECT().Client(sig.NodeID()).Return(postClient, nil)

	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		poetDb,
		postService,
		zaptest.NewLogger(t),
		PoetConfig{PhaseShift: layerDuration},
		mclock,
		WithPoetClients(poetProver),
	)
	require.NoError(t, err)

	mCertifier := NewMockcertifierService(ctrl)

	invalid := &certifier.PoetCert{}
	getInvalid := mCertifier.EXPECT().GetCertificate("http://localhost:9999").Return(invalid)
	submitFailed := poetProver.EXPECT().
		Submit(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			sig.NodeID(),
			PoetAuth{PoetCert: invalid},
		).
		After(getInvalid.Call).
		Return(nil, ErrUnathorized)

	valid := &certifier.PoetCert{Data: []byte("valid")}
	recertify := mCertifier.EXPECT().Recertify(gomock.Any(), poetProver).After(submitFailed).Return(valid, nil)
	submitOK := poetProver.EXPECT().
		Submit(
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			gomock.Any(),
			sig.NodeID(),
			PoetAuth{PoetCert: valid},
		).
		After(recertify).
		Return(&types.PoetRound{}, nil)

	poetProver.EXPECT().
		Proof(gomock.Any(), "").
		After(submitOK).
		Return(&types.PoetProofMessage{}, []types.Member{types.Member(challenge.Hash())}, nil)

	_, err = nb.BuildNIPost(context.Background(), sig, &challenge, mCertifier)
	require.NoError(t, err)
}

// TestNIPoSTBuilder_StaleChallenge checks if
// it properly detects that the challenge is stale and the poet round has already started.
func TestNIPoSTBuilder_StaleChallenge(t *testing.T) {
	t.Parallel()

	currLayer := types.EpochID(10).FirstLayer()
	genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	// Act & Verify
	t.Run("no requests, poet round started", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetClient(ctrl)
		poetProver.EXPECT().Address().Return("http://localhost:9999")
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			},
		).AnyTimes()
		postService := NewMockpostService(ctrl)

		nb, err := NewNIPostBuilder(
			localsql.InMemory(),
			poetDb,
			postService,
			zaptest.NewLogger(t),
			PoetConfig{},
			mclock,
			WithPoetClients(poetProver),
		)
		require.NoError(t, err)

		certifier := NewMockcertifierService(ctrl)
		challenge := types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch()}
		nipost, err := nb.BuildNIPost(context.Background(), sig, &challenge, certifier)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "poet round has already started")
		require.Nil(t, nipost)
	})
	t.Run("no response before deadline", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetClient(ctrl)
		poetProver.EXPECT().Address().Return("http://localhost:9999")
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			}).AnyTimes()
		postService := NewMockpostService(ctrl)

		db := localsql.InMemory()
		nb, err := NewNIPostBuilder(
			db,
			poetDb,
			postService,
			zaptest.NewLogger(t),
			PoetConfig{},
			mclock,
			WithPoetClients(poetProver),
		)
		require.NoError(t, err)

		challenge := &types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch() - 1}
		err = nipost.AddChallenge(db, sig.NodeID(), challenge)
		require.NoError(t, err)

		// successfully registered to at least one poet
		err = nipost.AddPoetRegistration(db, sig.NodeID(), nipost.PoETRegistration{
			ChallengeHash: challenge.Hash(),
			Address:       "http://poet1.com",
			RoundID:       "1",
			RoundEnd:      time.Now().Add(10 * time.Second),
		})
		require.NoError(t, err)

		certifier := NewMockcertifierService(ctrl)

		nipost, err := nb.BuildNIPost(context.Background(), sig, challenge, certifier)
		require.ErrorIs(t, err, ErrATXChallengeExpired)
		require.ErrorContains(t, err, "poet proof for pub epoch")
		require.Nil(t, nipost)
	})
	t.Run("too late for proof generation", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		poetDb := NewMockpoetDbAPI(ctrl)
		mclock := NewMocklayerClock(ctrl)
		poetProver := NewMockPoetClient(ctrl)
		poetProver.EXPECT().Address().Return("http://localhost:9999")
		mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
			func(got types.LayerID) time.Time {
				return genesis.Add(layerDuration * time.Duration(got))
			}).AnyTimes()
		postService := NewMockpostService(ctrl)

		db := localsql.InMemory()
		nb, err := NewNIPostBuilder(
			db,
			poetDb,
			postService,
			zaptest.NewLogger(t),
			PoetConfig{},
			mclock,
			WithPoetClients(poetProver),
		)
		require.NoError(t, err)

		challenge := &types.NIPostChallenge{PublishEpoch: currLayer.GetEpoch() - 1}
		err = nipost.AddChallenge(db, sig.NodeID(), challenge)
		require.NoError(t, err)

		// successfully registered to at least one poet
		err = nipost.AddPoetRegistration(db, sig.NodeID(), nipost.PoETRegistration{
			ChallengeHash: challenge.Hash(),
			Address:       "http://poet1.com",
			RoundID:       "1",
			RoundEnd:      time.Now().Add(10 * time.Second),
		})
		require.NoError(t, err)

		// received a proof from poet
		err = nipost.UpdatePoetProofRef(db, sig.NodeID(), [32]byte{1, 2, 3}, &types.MerkleProof{})
		require.NoError(t, err)

		certifier := NewMockcertifierService(ctrl)

		nipost, err := nb.BuildNIPost(context.Background(), sig, challenge, certifier)
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
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 1,
	}
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	proof := &types.PoetProofMessage{
		PoetProof: types.PoetProof{
			LeafCount: 777,
		},
	}
	cert := &certifier.PoetCert{}

	ctrl := gomock.NewController(t)
	poetDb := NewMockpoetDbAPI(ctrl)
	poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil)
	mclock := defaultLayerClockMock(ctrl)

	buildCtx, cancel := context.WithCancel(context.Background())

	poet := NewMockPoetClient(ctrl)
	poet.EXPECT().
		Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), sig.NodeID(), PoetAuth{PoetCert: cert}).
		DoAndReturn(func(
			_ context.Context,
			_ time.Time,
			_, _ []byte,
			_ types.EdSignature,
			_ types.NodeID,
			_ PoetAuth,
		) (*types.PoetRound, error) {
			cancel()
			return &types.PoetRound{}, context.Canceled
		})
	poet.EXPECT().Proof(gomock.Any(), "").Return(proof, []types.Member{types.Member(challenge.Hash())}, nil)
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
		poetDb,
		postService,
		zaptest.NewLogger(t),
		poetCfg,
		mclock,
		WithPoetClients(poet),
	)
	require.NoError(t, err)

	certifier := NewMockcertifierService(ctrl)
	certifier.EXPECT().GetCertificate("http://localhost:9999").Return(cert)

	// Act
	nipost, err := nb.BuildNIPost(buildCtx, sig, &challenge, certifier)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, nipost)

	// allow to submit in the second try
	poet.EXPECT().
		Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(&types.PoetRound{}, nil)

	certifier.EXPECT().GetCertificate("http://localhost:9999").Return(cert)
	nipost, err = nb.BuildNIPost(context.Background(), sig, &challenge, certifier)
	require.NoError(t, err)

	// Verify
	ref, _ := proof.Ref()
	require.Equal(t, ref[:], nipost.PostMetadata.Challenge)
}

func TestConstructingMerkleProof(t *testing.T) {
	challenge := types.NIPostChallenge{}
	challengeHash := challenge.Hash()

	t.Run("members empty", func(t *testing.T) {
		_, err := constructMerkleProof(challengeHash, []types.Member{})
		require.Error(t, err)
	})
	t.Run("not a member", func(t *testing.T) {
		_, err := constructMerkleProof(challengeHash, []types.Member{{}, {}})
		require.Error(t, err)
	})

	t.Run("is odd member", func(t *testing.T) {
		otherChallenge := types.NIPostChallenge{Sequence: 1}
		members := []types.Member{
			types.Member(challengeHash),
			types.Member(otherChallenge.Hash()),
		}
		proof, err := constructMerkleProof(challengeHash, members)
		require.NoError(t, err)

		root, err := calcRoot(members)
		require.NoError(t, err)

		err = validateMerkleProof(challengeHash[:], proof, root)
		require.NoError(t, err)
	})
	t.Run("is even member", func(t *testing.T) {
		otherChallenge := types.NIPostChallenge{Sequence: 1}
		members := []types.Member{
			types.Member(otherChallenge.Hash()),
			types.Member(challengeHash),
		}
		proof, err := constructMerkleProof(challengeHash, members)
		require.NoError(t, err)

		root, err := calcRoot(members)
		require.NoError(t, err)

		err = validateMerkleProof(challengeHash[:], proof, root)
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			challenge := types.NIPostChallenge{
				PublishEpoch: tc.epoch,
			}

			ctrl := gomock.NewController(t)
			poets := make([]PoetClient, 0, 2)
			{
				poetProvider := NewMockPoetClient(ctrl)
				poetProvider.EXPECT().Address().Return(tc.from)

				// PoET succeeds to submit
				poetProvider.EXPECT().
					Submit(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(&types.PoetRound{}, nil)

				// proof is fetched from PoET
				poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
					PoetProof: types.PoetProof{},
				}, []types.Member{types.Member(challenge.Hash())}, nil)
				poets = append(poets, poetProvider)
			}

			{
				// PoET fails submission
				poetProvider := NewMockPoetClient(ctrl)
				poetProvider.EXPECT().Address().Return(tc.to)

				// proof is still fetched from PoET
				poetProvider.EXPECT().Proof(gomock.Any(), "").Return(&types.PoetProofMessage{
					PoetProof: types.PoetProof{},
				}, []types.Member{types.Member(challenge.Hash())}, nil)

				poets = append(poets, poetProvider)
			}

			poetDb := NewMockpoetDbAPI(ctrl)
			poetDb.EXPECT().ValidateAndStore(gomock.Any(), gomock.Any()).Return(nil).Times(2)
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
				poetDb,
				postService,
				zaptest.NewLogger(t),
				poetCfg,
				mclock,
				WithPoetClients(poets...),
			)
			require.NoError(t, err)

			certifier := NewMockcertifierService(ctrl)
			certifier.EXPECT().CertifyAll(gomock.Any(), gomock.Any()).Return(nil)

			nipost, err := nb.BuildNIPost(context.Background(), sig, &challenge, certifier)
			require.NoError(t, err)
			require.NotNil(t, nipost)
		})
	}
}

func TestRandomDurationInRange(t *testing.T) {
	t.Parallel()
	test := func(min, max time.Duration) {
		for i := 0; i < 100; i++ {
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

	poet := defaultPoetServiceMock(ctrl, "http://localhost:9999")
	poet.EXPECT().Proof(gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(ctx context.Context, _ string) (*types.PoetProofMessage, []types.Member, error) {
			return nil, nil, ctx.Err()
		})
	nb, err := NewNIPostBuilder(
		localsql.InMemory(),
		NewMockpoetDbAPI(ctrl),
		NewMockpostService(ctrl),
		zaptest.NewLogger(t),
		PoetConfig{},
		defaultLayerClockMock(ctrl),
		WithPoetClients(poet),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	challenge := types.NIPostChallenge{
		PublishEpoch: postGenesisEpoch + 2,
	}

	mCertifier := NewMockcertifierService(ctrl)
	mCertifier.EXPECT().GetCertificate("http://localhost:9999").Return(&certifier.PoetCert{})

	_, err = nb.BuildNIPost(ctx, sig, &challenge, mCertifier)
	require.Error(t, err)
}
