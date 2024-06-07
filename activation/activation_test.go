package activation

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"testing"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	sqlmocks "github.com/spacemeshos/go-spacemesh/sql/mocks"
)

// ========== Vars / Consts ==========

const (
	layersPerEpoch                 = 10
	layerDuration                  = time.Second
	postGenesisEpoch types.EpochID = 2
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)
	res := m.Run()
	os.Exit(res)
}

// ========== Helper functions ==========

type testAtxBuilder struct {
	*Builder
	db          sql.Executor
	localDb     *localsql.Database
	goldenATXID types.ATXID

	observedLogs *observer.ObservedLogs
	mctrl        *gomock.Controller
	mpub         *mocks.MockPublisher
	mnipost      *MocknipostBuilder
	mpostClient  *MockPostClient
	mclock       *MocklayerClock
	msync        *Mocksyncer
	mValidator   *MocknipostValidator
}

func newTestBuilder(tb testing.TB, numSigners int, opts ...BuilderOption) *testAtxBuilder {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zaptest.NewLogger(tb, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	ctrl := gomock.NewController(tb)
	tab := &testAtxBuilder{
		db:          sql.InMemory(),
		localDb:     localsql.InMemory(sql.WithConnections(numSigners)),
		goldenATXID: types.ATXID(types.HexToHash32("77777")),

		observedLogs: observedLogs,
		mctrl:        ctrl,
		mpub:         mocks.NewMockPublisher(ctrl),
		mnipost:      NewMocknipostBuilder(ctrl),
		mpostClient:  NewMockPostClient(ctrl),
		mclock:       NewMocklayerClock(ctrl),
		msync:        NewMocksyncer(ctrl),
		mValidator:   NewMocknipostValidator(ctrl),
	}

	opts = append(opts, WithValidator(tab.mValidator))

	cfg := Config{
		GoldenATXID: tab.goldenATXID,
	}

	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan).AnyTimes()

	b := NewBuilder(
		cfg,
		tab.db,
		atxsdata.New(),
		tab.localDb,
		tab.mpub,
		tab.mnipost,
		tab.mclock,
		tab.msync,
		logger,
		opts...,
	)
	tab.Builder = b

	for range numSigners {
		sig, err := signing.NewEdSigner()
		require.NoError(tb, err)
		tab.Register(sig)
	}

	return tab
}

func publishAtxV1(
	tb testing.TB,
	tab *testAtxBuilder,
	nodeID types.NodeID,
	posEpoch types.EpochID,
	currLayer *types.LayerID,
	buildNIPostLayerDuration uint32,
) *wire.ActivationTxV1 {
	tb.Helper()
	var watx wire.ActivationTxV1
	publishAtx(tb, tab, nodeID, posEpoch, currLayer, buildNIPostLayerDuration,
		func(_ context.Context, _ string, got []byte) error {
			return codec.Decode(got, &watx)
		})
	require.NoError(tb, atxs.Add(tab.db, toAtx(tb, &watx), watx.Blob(), watx.PrevATXID))
	tab.atxsdata.AddFromAtx(toAtx(tb, &watx), false)
	return &watx
}

func publishAtx(
	tb testing.TB,
	tab *testAtxBuilder,
	nodeID types.NodeID,
	posEpoch types.EpochID,
	currLayer *types.LayerID, // pointer to keep current layer consistent across calls
	buildNIPostLayerDuration uint32,
	onPublish func(context.Context, string, []byte) error,
) {
	tb.Helper()

	require.Contains(tb, tab.signers, nodeID)

	publishEpoch := posEpoch + 1
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(*currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	nonce := types.VRFPostIndex(123)
	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        nodeID,
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
	}, nil).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *signing.EdSigner, _ types.Hash32, _ *types.NIPostChallenge,
		) (*nipost.NIPostState, error) {
			*currLayer = currLayer.Add(buildNIPostLayerDuration)
			return newNIPostWithPoet(tb, types.RandomHash().Bytes()), nil
		})
	ch := make(chan struct{})
	close(ch)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) <-chan struct{} {
			// advance to publish layer
			if currLayer.Before(got) {
				*currLayer = got
			}
			return ch
		})

	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(onPublish)
	tab.mnipost.EXPECT().ResetState(nodeID).Return(nil)
	// create and publish ATX
	err := tab.PublishActivationTx(context.Background(), tab.signers[nodeID])
	require.NoError(tb, err)
}

// ========== Tests ==========

func Test_Builder_StartSmeshingCoinbase(t *testing.T) {
	tab := newTestBuilder(t, 1)
	sig := maps.Values(tab.signers)[0]
	coinbase := types.Address{1, 1, 1}

	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
		) (*types.Post, *types.PostInfo, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		})
	tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()
	require.NoError(t, tab.StartSmeshing(coinbase))
	require.Equal(t, coinbase, tab.Coinbase())

	// calling StartSmeshing more than once before calling StopSmeshing is an error
	require.ErrorContains(t, tab.StartSmeshing(coinbase), "already started")

	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	require.NoError(t, tab.StopSmeshing(true))
}

func TestBuilder_RestartSmeshing(t *testing.T) {
	getBuilder := func(t *testing.T) *Builder {
		tab := newTestBuilder(t, 1)
		sig := maps.Values(tab.signers)[0]

		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).AnyTimes().DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
			) (*types.Post, *types.PostInfo, error) {
				<-ctx.Done()
				return nil, nil, ctx.Err()
			})

		ch := make(chan struct{})
		close(ch)
		tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(ch).AnyTimes()
		tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
		return tab.Builder
	}

	t.Run("Single threaded", func(t *testing.T) {
		builder := getBuilder(t)
		for range 50 {
			require.NoError(t, builder.StartSmeshing(types.Address{}))
			require.True(t, builder.Smeshing())
			require.NoError(t, builder.StopSmeshing(false))
			require.False(t, builder.Smeshing())
		}
	})

	t.Run("Multi threaded", func(t *testing.T) {
		// Meant to be run with -race to detect races.
		// It cannot check `builder.Smeshing()` as Start/Stop is happening from many goroutines simultaneously.
		// Both Start and Stop can fail as it is not known if builder is smeshing or not.
		builder := getBuilder(t)
		var eg errgroup.Group
		for worker := 0; worker < 10; worker += 1 {
			eg.Go(func() error {
				for range 50 {
					builder.StartSmeshing(types.Address{})
					builder.StopSmeshing(false)
				}
				return nil
			})
		}
		require.NoError(t, eg.Wait())
	})
}

func TestBuilder_StopSmeshing_Delete(t *testing.T) {
	tab := newTestBuilder(t, 1)
	sig := maps.Values(tab.signers)[0]

	atx := types.RandomATXID()
	refChallenge := &types.NIPostChallenge{
		PublishEpoch:  postGenesisEpoch + 2,
		CommitmentATX: &atx,
	}

	currLayer := (postGenesisEpoch + 1).FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer)
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{}))

	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
		) (*types.Post, *types.PostInfo, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		})

	// add challenge to DB
	require.NoError(t, nipost.AddChallenge(tab.localDb, sig.NodeID(), refChallenge))

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(false))
	require.True(t, tab.mctrl.Satisfied(), "failed to assert all mocks were called the expected number of times")

	tab.mclock.EXPECT().CurrentLayer().Return(currLayer)
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{}))

	challenge, err := nipost.Challenge(tab.localDb, sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, refChallenge, challenge) // challenge still present

	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
		) (*types.Post, *types.PostInfo, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		})

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true))
	require.True(t, tab.mctrl.Satisfied(), "failed to assert all mocks were called the expected number of times")

	tab.mclock.EXPECT().CurrentLayer().Return(currLayer)
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{}))

	challenge, err = nipost.Challenge(tab.localDb, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, challenge) // challenge deleted

	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte, _ *types.NIPostChallenge,
		) (*types.Post, *types.PostInfo, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		})
	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true)) // no-op
	require.True(t, tab.mctrl.Satisfied(), "failed to assert all mocks were called the expected number of times")

	challenge, err = nipost.Challenge(tab.localDb, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, challenge) // challenge still deleted
}

func TestBuilder_StopSmeshing_failsWhenNotStarted(t *testing.T) {
	tab := newTestBuilder(t, 1)
	require.ErrorContains(t, tab.StopSmeshing(true), "not started")
}

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, prevAtx), prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, prevAtx), false)

	// create and publish ATX
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(4)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), prevAtx.ID(), tab.goldenATXID, gomock.Any())
	atx1 := publishAtxV1(t, tab, sig.NodeID(), posEpoch, &currLayer, layersPerEpoch)
	require.NotNil(t, atx1)
	require.Equal(t, prevAtx.ID(), atx1.PositioningATXID)

	// create and publish another ATX
	currLayer = (posEpoch + 1).FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(4)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), atx1.ID(), tab.goldenATXID, gomock.Any())
	atx2 := publishAtxV1(t, tab, sig.NodeID(), atx1.PublishEpoch, &currLayer, layersPerEpoch)
	require.NotNil(t, atx2)
	require.NotEqual(t, atx1, atx2)
	require.Equal(t, atx1.PublishEpoch+1, atx2.PublishEpoch)
	require.Equal(t, atx1.ID(), atx2.PositioningATXID)

	// state is cleaned up
	_, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

// TestBuilder_Loop_WaitsOnStaleChallenge checks if loop waits between attempts
// failing with ErrATXChallengeExpired.
func TestBuilder_Loop_WaitsOnStaleChallenge(t *testing.T) {
	// Arrange
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	// current layer is too late to be able to build a nipost on time
	currLayer := (postGenesisEpoch + 1).FirstLayer()
	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, prevAtx), prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, prevAtx), false)

	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()

	tab.mnipost.EXPECT().
		BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, ErrATXChallengeExpired)
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tab.mclock.EXPECT().AwaitLayer(currLayer.Add(1)).Do(func(got types.LayerID) <-chan struct{} {
		cancel()
		ch := make(chan struct{})
		close(ch)
		return ch
	})

	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	// Act & Verify
	var eg errgroup.Group
	eg.Go(func() error {
		tab.run(ctx, sig)
		return nil
	})

	require.NoError(t, eg.Wait())

	// state is cleaned up
	_, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := postGenesisEpoch.FirstLayer()
	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, prevAtx), prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, prevAtx), false)

	publishEpoch := posEpoch + 1
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	nonce := types.VRFPostIndex(123)
	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        sig.NodeID(),
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
	}, nil).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *signing.EdSigner, _ types.Hash32, _ *types.NIPostChallenge,
		) (*nipost.NIPostState, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithPoet(t, []byte("66666")), nil
		})
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) <-chan struct{} {
			// advance to publish layer
			if currLayer.Before(got) {
				currLayer = got
			}
			return done
		})
	var published []byte
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		// first publish fails
		func(_ context.Context, _ string, got []byte) error {
			published = got
			return errors.New("something went wrong")
		},
	)

	// after successful publish, state is cleaned up
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		// second publish succeeds
		func(_ context.Context, _ string, got []byte) error {
			require.Equal(t, published, got)
			return nil
		},
	)
	// create and publish ATX
	require.NoError(t, tab.PublishActivationTx(context.Background(), sig))

	// state is cleaned up
	_, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_UsesExistingChallengeOnLatePublish(t *testing.T) {
	poetCfg := PoetConfig{
		PhaseShift: layerDuration * 4,
	}
	tab := newTestBuilder(t, 1, WithPoetConfig(poetCfg))
	sig := maps.Values(tab.signers)[0]

	currLayer := (postGenesisEpoch + 1).FirstLayer().Add(5) // late for poet round start
	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	vPrevAtx := toAtx(t, prevAtx)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx, prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, prevAtx), false)

	publishEpoch := currLayer.GetEpoch()
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	nonce := types.VRFPostIndex(123)
	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        sig.NodeID(),
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
	}, nil).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *signing.EdSigner, _ types.Hash32, _ *types.NIPostChallenge,
		) (*nipost.NIPostState, error) {
			currLayer = currLayer.Add(1)
			return newNIPostWithPoet(t, []byte("66666")), nil
		})
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) <-chan struct{} {
			// advance to publish layer
			if currLayer.Before(got) {
				currLayer = got
			}
			return done
		})

	// store challenge in DB
	ch := &types.NIPostChallenge{
		Sequence:       vPrevAtx.Sequence + 1,
		PrevATXID:      vPrevAtx.ID(),
		PublishEpoch:   vPrevAtx.PublishEpoch + 1,
		PositioningATX: vPrevAtx.ID(),
	}

	require.NoError(t, nipost.AddChallenge(tab.localDb, sig.NodeID(), ch))

	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)

	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		// publish succeeds
		func(_ context.Context, _ string, got []byte) error {
			var atx wire.ActivationTxV1
			codec.MustDecode(got, &atx)
			require.Equal(t, wire.NIPostChallengeToWireV1(ch), &atx.NIPostChallengeV1)
			return nil
		},
	)

	// create and publish ATX
	require.NoError(t, tab.PublishActivationTx(context.Background(), sig))

	// state is cleaned up
	_, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_RebuildNIPostWhenTargetEpochPassed(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := types.EpochID(2)
	currLayer := posEpoch.FirstLayer()
	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	vPrevAtx := toAtx(t, prevAtx)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx, prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, prevAtx), false)

	publishEpoch := posEpoch + 1
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(
		func() types.LayerID {
			return currLayer
		}).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *signing.EdSigner, _ types.Hash32, _ *types.NIPostChallenge,
		) (*nipost.NIPostState, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithPoet(t, []byte("66666")), nil
		})
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) <-chan struct{} {
			// advance to publish layer
			if currLayer.Before(got) {
				currLayer = got
			}
			return done
		})
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var built *wire.ActivationTxV1
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			built = new(wire.ActivationTxV1)
			codec.MustDecode(got, built)
			// advance time to the next epoch to trigger the context timeout
			currLayer = currLayer.Add(layersPerEpoch)
			cancel()
			return errors.New("something went wrong")
		},
	)
	// create and publish ATX
	err := tab.PublishActivationTx(ctx, sig)
	require.ErrorIs(t, err, context.Canceled) // publish returning an error will just cause a retry if not canceled
	require.NotNil(t, built)

	// state is preserved for a retry
	challenge, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, &built.NIPostChallengeV1, wire.NIPostChallengeToWireV1(challenge))

	// We started building the NIPost in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the publish epoch (3) has passed, so we'll start the ATX builder in epoch 4 and ensure it discards
	// a stale challenge and builds a new NIPost.
	posEpoch = types.EpochID(4)
	currLayer = posEpoch.FirstLayer()
	posAtx := newInitialATXv1(t, tab.goldenATXID, func(atx *wire.ActivationTxV1) { atx.PublishEpoch = posEpoch })
	posAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, posAtx), posAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, posAtx), false)
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), posAtx.ID(), tab.goldenATXID, gomock.Any())
	built2 := publishAtxV1(t, tab, sig.NodeID(), posEpoch, &currLayer, layersPerEpoch)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPostChallengeV1, built2.NIPostChallengeV1)
	require.Equal(t, posEpoch+1, built2.PublishEpoch)

	// state is cleaned up after successful publish
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()

	// generate and store initial post in state
	post := nipost.Post{
		Indices: types.RandomBytes(10),
		Nonce:   rand.Uint32(),
		Pow:     rand.Uint64(),

		NumUnits:      uint32(12),
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      types.VRFPostIndex(rand.Uint64()),
		Challenge:     shared.ZeroChallenge,
	}
	require.NoError(t, nipost.AddPost(tab.localDb, sig.NodeID(), post))
	initialPost := &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}
	tab.mValidator.EXPECT().
		PostV2(gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, shared.ZeroChallenge, post.NumUnits)

	// create and publish ATX
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	atx := publishAtxV1(t, tab, sig.NodeID(), posEpoch, &currLayer, layersPerEpoch)
	require.NotNil(t, atx)

	// state is cleaned up
	_, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_NoPrevATX_PublishFails_InitialPost_preserved(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()

	// generate and store initial refPost in state
	refPost := nipost.Post{
		Indices: types.RandomBytes(10),
		Nonce:   rand.Uint32(),
		Pow:     rand.Uint64(),

		NumUnits:      uint32(12),
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      types.VRFPostIndex(rand.Uint64()),
		Challenge:     shared.ZeroChallenge,
	}
	require.NoError(t, nipost.AddPost(tab.localDb, sig.NodeID(), refPost))
	initialPost := &types.Post{
		Nonce:   refPost.Nonce,
		Indices: refPost.Indices,
		Pow:     refPost.Pow,
	}

	tab.mValidator.EXPECT().
		PostV2(gomock.Any(), sig.NodeID(), refPost.CommitmentATX, initialPost, refPost.Challenge, refPost.NumUnits)

	// create and publish ATX
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	tab.mnipost.EXPECT().
		BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, ErrATXChallengeExpired)
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	ch := make(chan struct{})
	tab.mclock.EXPECT().AwaitLayer(currLayer.Add(1)).Do(func(got types.LayerID) <-chan struct{} {
		close(ch)
		return ch
	})

	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		tab.run(ctx, sig)
		return nil
	})
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, eg.Wait())
	})

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		require.FailNow(t, "timed out waiting for builder to publish ATX")
	}

	// initial post is preserved
	post, err := nipost.GetPost(tab.localDB, sig.NodeID())
	require.NoError(t, err)
	require.NotNil(t, post)
	require.Equal(t, refPost, *post)

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_PrevATXWithoutPrevATX(t *testing.T) {
	r := require.New(t)

	// Arrange
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	otherSigner, err := signing.NewEdSigner()
	r.NoError(err)

	poetBytes := []byte("poet")
	currentLayer := postGenesisEpoch.FirstLayer().Add(3)
	posAtx := newInitialATXv1(t, tab.goldenATXID)
	posAtx.Sign(otherSigner)
	vPosAtx := toAtx(t, posAtx)
	vPosAtx.TickCount = 100
	r.NoError(atxs.Add(tab.db, vPosAtx, posAtx.Blob()))
	tab.atxsdata.AddFromAtx(vPosAtx, false)

	nonce := types.VRFPostIndex(123)
	prevAtx := newInitialATXv1(t, tab.goldenATXID, func(atx *wire.ActivationTxV1) {
		atx.VRFNonce = (*uint64)(&nonce)
	})
	prevAtx.Sign(sig)
	vPrevAtx := toAtx(t, prevAtx)
	r.NoError(atxs.Add(tab.db, vPrevAtx, prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(vPrevAtx, false)

	// Act
	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan).AnyTimes()

	tab.mclock.EXPECT().CurrentLayer().Return(currentLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(layer types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currentLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(layer))
		}).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(vPosAtx.PublishEpoch.FirstLayer().Add(layersPerEpoch)).DoAndReturn(
		func(layer types.LayerID) <-chan struct{} {
			ch := make(chan struct{})
			close(ch)
			return ch
		},
	)

	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        sig.NodeID(),
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
	}, nil).AnyTimes()

	tab.mnipost.EXPECT().
		BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(
			func(_ context.Context, _ *signing.EdSigner, _ types.Hash32, _ *types.NIPostChallenge,
			) (*nipost.NIPostState, error) {
				currentLayer = currentLayer.Add(5)
				return newNIPostWithPoet(t, poetBytes), nil
			})

	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	tab.mpub.EXPECT().
		Publish(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, msg []byte) error {
			var atx wire.ActivationTxV1
			codec.MustDecode(msg, &atx)

			r.Equal(sig.NodeID(), atx.SmesherID)
			r.Equal(prevAtx.Sequence+1, atx.Sequence)
			r.Equal(prevAtx.ID(), atx.PrevATXID)
			r.Nil(atx.InitialPost)
			r.Nil(atx.CommitmentATXID)
			r.Nil(atx.VRFNonce)
			r.Equal(posAtx.ID(), atx.PositioningATXID)
			r.Equal(prevAtx.PublishEpoch+1, atx.PublishEpoch)
			r.Equal(poetBytes, atx.NIPost.PostMetadata.Challenge)
			return nil
		})

	tab.mnipost.EXPECT().ResetState(sig.NodeID())

	r.NoError(tab.PublishActivationTx(context.Background(), sig))

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_TargetsEpochBasedOnPosAtx(t *testing.T) {
	r := require.New(t)

	// Arrange
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	otherSigner, err := signing.NewEdSigner()
	r.NoError(err)

	poetBytes := []byte("poet")
	currentLayer := postGenesisEpoch.FirstLayer().Add(3)
	posEpoch := postGenesisEpoch
	posAtx := newInitialATXv1(t, tab.goldenATXID)
	posAtx.Sign(otherSigner)
	r.NoError(atxs.Add(tab.db, toAtx(t, posAtx), posAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, posAtx), false)

	// Act & Assert
	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan).AnyTimes()

	tab.mclock.EXPECT().CurrentLayer().Return(currentLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(layer types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currentLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(layer))
		}).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(posAtx.PublishEpoch.FirstLayer().Add(layersPerEpoch)).DoAndReturn(
		func(types.LayerID) <-chan struct{} {
			ch := make(chan struct{})
			close(ch)
			return ch
		},
	)

	nonce := types.VRFPostIndex(123)
	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        sig.NodeID(),
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
	}, nil).AnyTimes()

	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ *signing.EdSigner, _ types.Hash32, _ *types.NIPostChallenge,
		) (*nipost.NIPostState, error) {
			currentLayer = currentLayer.Add(layersPerEpoch)
			return newNIPostWithPoet(t, poetBytes), nil
		})

	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	tab.mpub.EXPECT().
		Publish(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, msg []byte) error {
			var atx wire.ActivationTxV1
			codec.MustDecode(msg, &atx)

			r.Equal(sig.NodeID(), atx.SmesherID)
			r.Zero(atx.Sequence)
			r.Equal(types.EmptyATXID, atx.PrevATXID)
			r.NotNil(atx.InitialPost)
			r.Equal(posAtx.ID(), atx.PositioningATXID)
			r.Equal(posEpoch+1, atx.PublishEpoch)
			r.Equal(poetBytes, atx.NIPost.PostMetadata.Challenge)

			return nil
		})

	post := nipost.Post{
		Indices: types.RandomBytes(10),
		Nonce:   rand.Uint32(),
		Pow:     rand.Uint64(),

		NumUnits:      uint32(12),
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      types.VRFPostIndex(rand.Uint64()),
		Challenge:     shared.ZeroChallenge,
	}
	require.NoError(t, nipost.AddPost(tab.localDb, sig.NodeID(), post))
	initialPost := &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}

	tab.mValidator.EXPECT().
		PostV2(gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, shared.ZeroChallenge, post.NumUnits)
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)

	r.NoError(tab.PublishActivationTx(context.Background(), sig))

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_FailsWhenNIPostBuilderFails(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, prevAtx), prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, prevAtx), false)

	tab.mclock.EXPECT().CurrentLayer().Return(posEpoch.FirstLayer()).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	nipostErr := errors.New("NIPost builder error")
	tab.mnipost.EXPECT().
		BuildNIPost(gomock.Any(), sig, gomock.Any(), gomock.Any()).
		Return(nil, nipostErr)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	require.ErrorIs(t, tab.PublishActivationTx(context.Background(), sig), nipostErr)

	// state is preserved
	challenge, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.NoError(t, err)
	require.NotNil(t, challenge)
}

func TestBuilder_SignAtx(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	atx := newInitialATXv1(t, types.ATXID(types.HexToHash32("0x1234")))
	atx.Sign(sig)

	bytes := atx.SignedBytes()
	ok := signing.NewEdVerifier().Verify(signing.ATX, sig.NodeID(), bytes, atx.Signature)
	require.True(t, ok)
	require.Equal(t, sig.NodeID(), atx.SmesherID)
}

func TestBuilder_RetryPublishActivationTx(t *testing.T) {
	events.InitializeReporter()
	sub, err := events.SubscribeMatched(func(t *events.UserEvent) bool {
		switch t.Event.Details.(type) {
		case *pb.Event_AtxPublished:
			return true
		default:
			return false
		}
	}, events.WithBuffer(100))
	require.NoError(t, err)

	retryInterval := 50 * time.Microsecond
	tab := newTestBuilder(
		t,
		1,
		WithPoetConfig(PoetConfig{PhaseShift: 150 * time.Millisecond}),
		WithPoetRetryInterval(retryInterval),
	)
	sig := maps.Values(tab.signers)[0]
	prevAtx := newInitialATXv1(t, tab.goldenATXID)
	prevAtx.Sign(sig)
	require.NoError(t, atxs.Add(tab.db, toAtx(t, prevAtx), prevAtx.Blob()))
	tab.atxsdata.AddFromAtx(toAtx(t, prevAtx), false)

	currLayer := prevAtx.PublishEpoch.FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(prevAtx.PublishEpoch.Add(1).FirstLayer()).DoAndReturn(
		func(got types.LayerID) <-chan struct{} {
			// advance to publish layer
			if currLayer.Before(got) {
				currLayer = got
			}
			return done
		},
	)

	expectedTries := 3
	tries := 0
	var last time.Time
	builderConfirmation := make(chan struct{})
	tab.mnipost.EXPECT().
		BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Times(expectedTries).
		DoAndReturn(
			// nolint:lll
			func(_ context.Context, _ *signing.EdSigner, _ types.Hash32, _ *types.NIPostChallenge) (*nipost.NIPostState, error) {
				now := time.Now()
				if now.Sub(last) < retryInterval {
					require.FailNow(t, "retry interval not respected")
				}

				tries++
				t.Logf("try %d: %s", tries, now)
				if tries < expectedTries {
					return nil, ErrPoetServiceUnstable
				}
				close(builderConfirmation)
				return newNIPostWithPoet(t, []byte("66666")), nil
			},
		)

	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	nonce := types.VRFPostIndex(123)
	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        sig.NodeID(),
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
	}, nil).AnyTimes()

	var publishedID types.ATXID
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(ctx context.Context, s string, b []byte) error {
			var atx wire.ActivationTxV1
			codec.MustDecode(b, &atx)
			publishedID = atx.ID()

			// advance time to the next epoch
			currLayer = currLayer.Add(layersPerEpoch)
			return nil
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eg errgroup.Group
	eg.Go(func() error {
		tab.run(ctx, sig)
		return nil
	})
	t.Cleanup(func() { assert.NoError(t, eg.Wait()) })

	select {
	case <-builderConfirmation:
	case <-time.After(5 * time.Second):
		require.FailNowf(t, "failed waiting for required number of tries", "only tried %d times", tries)
	}

	select {
	case ev := <-sub.Out():
		cancel()
		atxEvent := ev.Event.GetAtxPublished()
		require.Equal(t, publishedID, types.BytesToATXID(atxEvent.GetId()))
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for activation event")
	}

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_InitialProofGeneratedOnce(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	post := nipost.Post{
		Indices: types.RandomBytes(10),
		Nonce:   rand.Uint32(),
		Pow:     rand.Uint64(),

		NumUnits:      uint32(12),
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      types.VRFPostIndex(rand.Uint64()),
	}
	initialPost := &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}

	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: post.CommitmentATX,
			Nonce:         &post.VRFNonce,

			NumUnits:      post.NumUnits,
			LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
		},
		nil,
	)
	tab.mValidator.EXPECT().
		PostV2(gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, shared.ZeroChallenge, post.NumUnits)

	require.NoError(t, tab.BuildInitialPost(context.Background(), sig.NodeID()))
	// postClient.Proof() should not be called again
	require.NoError(t, tab.BuildInitialPost(context.Background(), sig.NodeID()))
}

func TestBuilder_InitialPostIsPersisted(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	commitmentATX := types.RandomATXID()
	nonce := types.VRFPostIndex(rand.Uint64())
	numUnits := uint32(12)
	initialPost := &types.Post{
		Nonce:   rand.Uint32(),
		Indices: types.RandomBytes(10),
		Pow:     rand.Uint64(),
	}
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: commitmentATX,
			Nonce:         &nonce,

			NumUnits:      numUnits,
			LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
		},
		nil,
	)
	tab.mValidator.EXPECT().
		PostV2(gomock.Any(), sig.NodeID(), commitmentATX, initialPost, shared.ZeroChallenge, numUnits)

	require.NoError(t, tab.BuildInitialPost(context.Background(), sig.NodeID()))

	// postClient.Proof() should not be called again
	require.NoError(t, tab.BuildInitialPost(context.Background(), sig.NodeID()))
}

func TestBuilder_InitialPostLogErrorMissingVRFNonce(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	commitmentATX := types.RandomATXID()
	numUnits := uint32(12)
	initialPost := &types.Post{
		Nonce:   rand.Uint32(),
		Indices: types.RandomBytes(10),
		Pow:     rand.Uint64(),
	}
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, gomock.Any()).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: commitmentATX,

			NumUnits:      numUnits,
			LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
		},
		nil,
	)
	tab.mValidator.EXPECT().
		PostV2(gomock.Any(), sig.NodeID(), commitmentATX, initialPost, shared.ZeroChallenge, numUnits)
	err := tab.BuildInitialPost(context.Background(), sig.NodeID())
	require.ErrorIs(t, err, errNilVrfNonce)

	observedLogs := tab.observedLogs.FilterLevelExact(zapcore.ErrorLevel)
	require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
	require.Equal(t, zapcore.ErrorLevel, observedLogs.All()[0].Level)
	require.Equal(t, "initial PoST is invalid: missing VRF nonce. Check your PoST data", observedLogs.All()[0].Message)
	require.Equal(t, sig.NodeID().ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])

	// postClient.Proof() should be called again and no error if vrf nonce is provided
	nonce := types.VRFPostIndex(rand.Uint64())
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: commitmentATX,
			Nonce:         &nonce,

			NumUnits:      numUnits,
			LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
		},
		nil,
	)
	require.NoError(t, tab.BuildInitialPost(context.Background(), sig.NodeID()))
}

func TestWaitPositioningAtx(t *testing.T) {
	genesis := time.Now()
	for _, tc := range []struct {
		desc         string
		shift, grace time.Duration

		targetEpoch types.EpochID
	}{
		{"no wait", 200 * time.Millisecond, 200 * time.Millisecond, 2},
		{"wait", 200 * time.Millisecond, 0, 2},
		{"round started", 0, 0, 3},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{
				PhaseShift:  tc.shift,
				GracePeriod: tc.grace,
			}))
			sig := maps.Values(tab.signers)[0]

			tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
			tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(func(lid types.LayerID) time.Time {
				// layer duration is 10ms to speed up test
				return genesis.Add(time.Duration(lid) * 20 * time.Millisecond)
			}).AnyTimes()

			// everything else are stubs that are irrelevant for the test
			tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{}, nil).AnyTimes()
			tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
			tab.mnipost.EXPECT().
				BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&nipost.NIPostState{}, nil)

			closed := make(chan struct{})
			close(closed)

			tab.mclock.EXPECT().AwaitLayer(types.EpochID(1).FirstLayer()).Return(closed).AnyTimes()
			tab.mclock.EXPECT().AwaitLayer(types.EpochID(2).FirstLayer()).Return(closed).AnyTimes()

			tab.mpub.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ string, got []byte) error {
					var atx wire.ActivationTxV1
					codec.MustDecode(got, &atx)
					require.Equal(t, tc.targetEpoch, atx.PublishEpoch+1)
					return nil
				})

			post := nipost.Post{
				Indices: types.RandomBytes(10),
				Nonce:   rand.Uint32(),
				Pow:     rand.Uint64(),

				NumUnits:      uint32(12),
				CommitmentATX: types.RandomATXID(),
				VRFNonce:      types.VRFPostIndex(rand.Uint64()),
				Challenge:     shared.ZeroChallenge,
			}
			require.NoError(t, nipost.AddPost(tab.localDb, sig.NodeID(), post))
			initialPost := &types.Post{
				Nonce:   post.Nonce,
				Indices: post.Indices,
				Pow:     post.Pow,
			}
			tab.mValidator.EXPECT().
				PostV2(gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, post.Challenge, post.NumUnits)

			require.NoError(t, tab.PublishActivationTx(context.Background(), sig))
		})
	}
}

// Test if GetPositioningAtx disregards ATXs with invalid POST in their chain.
// It should pick an ATX with valid POST even though it's a lower height.
func TestGetPositioningAtxPicksAtxWithValidChain(t *testing.T) {
	tab := newTestBuilder(t, 1)
	sig := maps.Values(tab.signers)[0]

	// Invalid chain with high height
	sigInvalid, err := signing.NewEdSigner()
	require.NoError(t, err)
	invalidAtx := newInitialATXv1(t, tab.goldenATXID)
	invalidAtx.Sign(sigInvalid)
	vInvalidAtx := toAtx(t, invalidAtx)
	vInvalidAtx.TickCount = 100
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vInvalidAtx, invalidAtx.Blob()))
	tab.atxsdata.AddFromAtx(vInvalidAtx, false)

	// Valid chain with lower height
	sigValid, err := signing.NewEdSigner()
	require.NoError(t, err)
	validAtx := newInitialATXv1(t, tab.goldenATXID)
	validAtx.NumUnits += 10
	validAtx.Sign(sigValid)
	vValidAtx := toAtx(t, validAtx)
	require.NoError(t, atxs.Add(tab.db, vValidAtx, validAtx.Blob()))
	tab.atxsdata.AddFromAtx(vValidAtx, false)

	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), invalidAtx.ID(), tab.goldenATXID, gomock.Any()).
		Return(errors.New(""))
	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), validAtx.ID(), tab.goldenATXID, gomock.Any())

	posAtxID, err := tab.getPositioningAtx(context.Background(), sig.NodeID(), 77, nil)
	require.NoError(t, err)
	require.Equal(t, posAtxID, vValidAtx.ID())

	// should use the cached positioning ATX when asked for the same publish epoch
	posAtxID, err = tab.getPositioningAtx(context.Background(), sig.NodeID(), 77, nil)
	require.NoError(t, err)
	require.Equal(t, posAtxID, vValidAtx.ID())

	// should lookup again when asked for a different publish epoch
	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), invalidAtx.ID(), tab.goldenATXID, gomock.Any()).
		Return(errors.New(""))
	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), validAtx.ID(), tab.goldenATXID, gomock.Any())

	posAtxID, err = tab.getPositioningAtx(context.Background(), sig.NodeID(), 99, nil)
	require.NoError(t, err)
	require.Equal(t, posAtxID, vValidAtx.ID())
}

func TestGetPositioningAtx(t *testing.T) {
	t.Parallel()
	t.Run("db failed", func(t *testing.T) {
		t.Parallel()
		tab := newTestBuilder(t, 1)

		db := sqlmocks.NewMockExecutor(gomock.NewController(t))
		tab.Builder.db = db
		expected := errors.New("db error")
		db.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(0, expected)

		none, err := tab.getPositioningAtx(context.Background(), types.EmptyNodeID, 99, nil)
		require.ErrorIs(t, err, expected)
		require.Equal(t, types.ATXID{}, none)
	})
	t.Run("picks golden if no ATXs", func(t *testing.T) {
		tab := newTestBuilder(t, 1)
		atx, err := tab.getPositioningAtx(context.Background(), types.EmptyNodeID, 99, nil)
		require.NoError(t, err)
		require.Equal(t, tab.goldenATXID, atx)
	})
	t.Run("prefers own previous to golden", func(t *testing.T) {
		prev := &types.ActivationTx{}
		prev.SetID(types.RandomATXID())
		tab := newTestBuilder(t, 1)
		atx, err := tab.getPositioningAtx(context.Background(), types.EmptyNodeID, 99, prev)
		require.NoError(t, err)
		require.Equal(t, prev.ID(), atx)
	})
	t.Run("prefers own previous when it has GTE ticks", func(t *testing.T) {
		tab := newTestBuilder(t, 1)

		atxInDb := &types.ActivationTx{TickCount: 10}
		atxInDb.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tab.db, atxInDb, types.AtxBlob{}))
		tab.atxsdata.AddFromAtx(atxInDb, false)

		prev := &types.ActivationTx{TickCount: 100}
		prev.SetID(types.RandomATXID())

		tab.mValidator.EXPECT().VerifyChain(gomock.Any(), atxInDb.ID(), tab.goldenATXID, gomock.Any())
		found, err := tab.searchPositioningAtx(context.Background(), types.EmptyNodeID, 99)
		require.NoError(t, err)
		require.Equal(t, atxInDb.ID(), found)

		// prev.Height > found.Height
		selected, err := tab.getPositioningAtx(context.Background(), types.EmptyNodeID, 99, prev)
		require.NoError(t, err)
		require.Equal(t, prev.ID(), selected)

		// prev.Height == found.Height
		prev.TickCount = atxInDb.TickCount
		selected, err = tab.getPositioningAtx(context.Background(), types.EmptyNodeID, 99, prev)
		require.NoError(t, err)
		require.Equal(t, prev.ID(), selected)
	})
	t.Run("prefers own previous or golded when positioning ATX selection timout expired", func(t *testing.T) {
		tab := newTestBuilder(t, 1)

		atxInDb := &types.ActivationTx{TickCount: 100}
		atxInDb.SetID(types.RandomATXID())
		require.NoError(t, atxs.Add(tab.db, atxInDb, types.AtxBlob{}))
		tab.atxsdata.AddFromAtx(atxInDb, false)

		prev := &types.ActivationTx{TickCount: 90}
		prev.SetID(types.RandomATXID())

		// no timeout set up
		tab.mValidator.EXPECT().VerifyChain(gomock.Any(), atxInDb.ID(), tab.goldenATXID, gomock.Any())
		found, err := tab.getPositioningAtx(context.Background(), types.EmptyNodeID, 99, prev)
		require.NoError(t, err)
		require.Equal(t, atxInDb.ID(), found)

		tab.posAtxFinder.found = nil

		// timeout set up, prev ATX exists
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		selected, err := tab.getPositioningAtx(ctx, types.EmptyNodeID, 99, prev)
		require.NoError(t, err)
		require.Equal(t, prev.ID(), selected)

		tab.posAtxFinder.found = nil

		// timeout set up, prev ATX do not exists
		ctx, cancel = context.WithCancel(context.Background())
		cancel()

		selected, err = tab.getPositioningAtx(ctx, types.EmptyNodeID, 99, nil)
		require.NoError(t, err)
		require.Equal(t, tab.goldenATXID, selected)
	})
}

func TestFindFullyValidHighTickAtx(t *testing.T) {
	t.Parallel()
	golden := types.RandomATXID()

	t.Run("skips malicious ATXs", func(t *testing.T) {
		data := atxsdata.New()
		atxMal := &types.ActivationTx{TickCount: 100, SmesherID: types.RandomNodeID()}
		atxMal.SetID(types.RandomATXID())
		data.AddFromAtx(atxMal, true)

		atxLower := &types.ActivationTx{TickCount: 10, SmesherID: types.RandomNodeID()}
		atxLower.SetID(types.RandomATXID())
		data.AddFromAtx(atxLower, false)

		mValidator := NewMocknipostValidator(gomock.NewController(t))
		mValidator.EXPECT().VerifyChain(gomock.Any(), atxLower.ID(), golden, gomock.Any())

		lg := zaptest.NewLogger(t)
		found, err := findFullyValidHighTickAtx(context.Background(), data, 0, golden, mValidator, lg)
		require.NoError(t, err)
		require.Equal(t, atxLower.ID(), found)
	})
}

// Test_Builder_RegenerateInitialPost tests the coverage for the edge case
// where a node operator may change SUs after creating the initial PoST but before
// submitting the first ATX, which should result in the initial PoST to be deleted
// and for the new PoST to be generated instead (this also loses the eligibility
// for the current epoch). This behavior is mocked by mocking the response of the
// proof validator.
func Test_Builder_RegenerateInitialPost(t *testing.T) {
	var (
		tab           = newTestBuilder(t, 1)
		sig           = maps.Values(tab.signers)[0]
		genesis       = time.Now()
		ctx, cancel   = context.WithCancel(context.Background())
		commitmentATX = types.RandomATXID()
		nonce         = types.VRFPostIndex(rand.Uint64())
		numUnits      = uint32(12)
		initialPost   = &types.Post{
			Nonce:   rand.Uint32(),
			Indices: types.RandomBytes(10),
			Pow:     rand.Uint64(),
		}
	)

	tab.mValidator.EXPECT().
		PostV2(gomock.Any(), sig.NodeID(), commitmentATX, initialPost, shared.ZeroChallenge, numUnits).
		Return(nil).Times(4)

	tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).DoAndReturn(func(id types.LayerID) <-chan struct{} {
		// this is our way to end the test - the code will wait on AwaitLayer in PublishActivationTx while
		// waiting for the publication epoch. otherwise the `run` method will keep on looping to broadcast
		// future atxs
		cancel()
		return make(chan struct{})
	}).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(func(lid types.LayerID) time.Time {
		// layer duration is 10ms to speed up test
		return genesis.Add(time.Duration(lid) * 20 * time.Millisecond)
	}).AnyTimes()
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil).Times(1)

	tab.mnipost.EXPECT().
		BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, ErrInvalidInitialPost)
	tab.mnipost.EXPECT().
		BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&nipost.NIPostState{}, nil)

	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge, nil).Return(initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: commitmentATX,
			Nonce:         &nonce,

			NumUnits:      numUnits,
			LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
		},
		nil,
	).Times(2)

	var eg errgroup.Group
	eg.Go(func() error {
		tab.run(ctx, sig)
		return nil
	})
	t.Cleanup(func() { assert.NoError(t, eg.Wait()) })
}
