package activation

import (
	"context"
	"errors"
	"fmt"
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
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
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

func newAtx(
	challenge types.NIPostChallenge,
	nipost *types.NIPost,
	numUnits uint32,
	coinbase types.Address,
) *types.ActivationTx {
	atx := types.NewActivationTx(challenge, coinbase, nipost, numUnits, nil)
	atx.SetEffectiveNumUnits(numUnits)
	atx.SetReceived(time.Now())
	atx.SetValidity(types.Valid)
	return atx
}

type atxOption func(*types.ActivationTx)

func withVrfNonce(nonce types.VRFPostIndex) atxOption {
	return func(atx *types.ActivationTx) {
		atx.VRFNonce = &nonce
	}
}

func newActivationTx(
	tb testing.TB,
	sig *signing.EdSigner,
	sequence uint64,
	prevATX types.ATXID,
	positioningATX types.ATXID,
	cATX *types.ATXID,
	publishEpoch types.EpochID,
	startTick, numTicks uint64,
	coinbase types.Address,
	numUnits uint32,
	nipost *types.NIPost,
	opts ...atxOption,
) *types.VerifiedActivationTx {
	challenge := types.NIPostChallenge{
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PublishEpoch:   publishEpoch,
		PositioningATX: positioningATX,
		CommitmentATX:  cATX,
	}
	atx := newAtx(challenge, nipost, numUnits, coinbase)
	if sequence == 0 {
		nodeID := sig.NodeID()
		atx.NodeID = &nodeID
	}

	atx.SetEffectiveNumUnits(numUnits)
	atx.SetReceived(time.Now())
	for _, opt := range opts {
		opt(atx)
	}
	require.NoError(tb, SignAndFinalizeAtx(sig, atx))
	vAtx, err := atx.Verify(startTick, numTicks)
	require.NoError(tb, err)
	return vAtx
}

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
	observer, observedLogs := observer.New(zapcore.DebugLevel)
	logger := zap.New(observer)

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
		GoldenATXID:   tab.goldenATXID,
		LabelsPerUnit: DefaultPostConfig().LabelsPerUnit,
	}

	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan).AnyTimes()

	b := NewBuilder(
		cfg,
		tab.db,
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

func publishAtx(
	tb testing.TB,
	tab *testAtxBuilder,
	nodeID types.NodeID,
	posEpoch types.EpochID,
	currLayer *types.LayerID, // pointer to keep current layer consistent across calls
	buildNIPostLayerDuration uint32,
) (*types.ActivationTx, error) {
	tb.Helper()

	if _, ok := tab.signers[nodeID]; !ok {
		return nil, fmt.Errorf("node %v not registered", nodeID)
	}

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
		func(_ context.Context, _ *signing.EdSigner, _ types.EpochID, _ types.Hash32) (*nipost.NIPostState, error) {
			*currLayer = currLayer.Add(buildNIPostLayerDuration)
			return newNIPostWithPoet(tb, []byte("66666")), nil
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
	var built *types.ActivationTx
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			gotAtx, err := wire.ActivationTxFromBytes(got)
			require.NoError(tb, err)
			gotAtx.SetReceived(time.Now().Local())
			built = gotAtx
			built.SetEffectiveNumUnits(gotAtx.NumUnits)
			vatx, err := built.Verify(0, 1)
			require.NoError(tb, err)
			require.NoError(tb, atxs.Add(tab.db, vatx))
			return nil
		})

	tab.mnipost.EXPECT().ResetState(nodeID).Return(nil)
	// Expect verification of positioning ATX candidate chain.
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	// create and publish ATX
	err := tab.PublishActivationTx(context.Background(), tab.signers[nodeID])
	return built, err
}

// ========== Tests ==========

func Test_Builder_StartSmeshingCoinbase(t *testing.T) {
	tab := newTestBuilder(t, 1)
	sig := maps.Values(tab.signers)[0]
	coinbase := types.Address{1, 1, 1}

	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
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

		tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).AnyTimes().DoAndReturn(
			func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
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

	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
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
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
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
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).DoAndReturn(
		func(ctx context.Context, _ types.NodeID, _ []byte) (*types.Post, *types.PostInfo, error) {
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
	ch := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   posEpoch,
		PositioningATX: tab.goldenATXID,
		CommitmentATX:  &tab.goldenATXID,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	prevAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx))

	// create and publish ATX
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(4)
	atx1, err := publishAtx(t, tab, sig.NodeID(), posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx1)

	// create and publish another ATX
	currLayer = (posEpoch + 1).FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(4)
	atx2, err := publishAtx(t, tab, sig.NodeID(), atx1.PublishEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx2)
	require.NotEqual(t, atx1, atx2)
	require.Equal(t, atx1.TargetEpoch()+1, atx2.TargetEpoch())

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
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
	ch := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   postGenesisEpoch,
		PositioningATX: tab.goldenATXID,
		CommitmentATX:  &tab.goldenATXID,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	prevAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx))

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
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := postGenesisEpoch.FirstLayer()
	ch := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   postGenesisEpoch,
		PositioningATX: tab.goldenATXID,
		CommitmentATX:  &tab.goldenATXID,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	prevAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx))

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
		func(_ context.Context, _ *signing.EdSigner, _ types.EpochID, _ types.Hash32) (*nipost.NIPostState, error) {
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
	var built *types.ActivationTx
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		// first publish fails
		func(_ context.Context, _ string, got []byte) error {
			gotAtx, err := wire.ActivationTxFromBytes(got)
			require.NoError(t, err)
			built = gotAtx
			built.SetReceived(time.Now().Local())
			return errors.New("something went wrong")
		},
	)

	// after successful publish, state is cleaned up
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		// second publish succeeds
		func(_ context.Context, _ string, got []byte) error {
			atx, err := wire.ActivationTxFromBytes(got)
			require.NoError(t, err)
			atx.SetReceived(built.Received())
			require.Equal(t, built, atx)
			return nil
		},
	)
	// create and publish ATX
	require.NoError(t, tab.PublishActivationTx(context.Background(), sig))

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_UsesExistingChallengeOnLatePublish(t *testing.T) {
	poetCfg := PoetConfig{
		PhaseShift: layerDuration * 4,
	}
	tab := newTestBuilder(t, 1, WithPoetConfig(poetCfg))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := (postGenesisEpoch + 1).FirstLayer().Add(5) // late for poet round start
	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   postGenesisEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  nil,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	prevAtx := newAtx(challenge, nipostData.NIPost, posEpoch.Uint32(), types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx))

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
		func(_ context.Context, _ *signing.EdSigner, _ types.EpochID, _ types.Hash32) (*nipost.NIPostState, error) {
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
			gotAtx, err := wire.ActivationTxFromBytes(got)
			require.NoError(t, err)
			gotAtx.SetReceived(time.Now().Local())
			require.Equal(t, *ch, gotAtx.NIPostChallenge)
			return nil
		},
	)

	// create and publish ATX
	require.NoError(t, tab.PublishActivationTx(context.Background(), sig))

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_RebuildNIPostWhenTargetEpochPassed(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := types.EpochID(2)
	currLayer := posEpoch.FirstLayer()
	ch := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   posEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  nil,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	prevAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx))

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
		func(_ context.Context, _ *signing.EdSigner, _ types.EpochID, _ types.Hash32) (*nipost.NIPostState, error) {
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
	var built *types.ActivationTx
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			gotAtx, err := wire.ActivationTxFromBytes(got)
			require.NoError(t, err)
			gotAtx.SetReceived(time.Now().Local())
			built = gotAtx

			// advance time to the next epoch to trigger the context timeout
			currLayer = currLayer.Add(layersPerEpoch)
			cancel()
			return errors.New("something went wrong")
		},
	)
	// create and publish ATX
	err = tab.PublishActivationTx(ctx, sig)
	require.ErrorIs(t, err, context.Canceled) // publish returning an error will just cause a retry if not canceled
	require.NotNil(t, built)

	// state is preserved for a retry
	challenge, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.NoError(t, err)
	require.Equal(t, built.NIPostChallenge, *challenge)

	// We started building the NIPost in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the publish epoch (3) has passed, so we'll start the ATX builder in epoch 4 and ensure it discards
	// a stale challenge and builds a new NIPost.
	posEpoch = types.EpochID(4)
	currLayer = posEpoch.FirstLayer()
	ch = types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   posEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  nil,
	}
	posAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, posAtx))
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPosAtx))
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)
	built2, err := publishAtx(t, tab, sig.NodeID(), posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPostChallenge, built2.NIPostChallenge)
	require.Equal(t, posEpoch+2, built2.TargetEpoch())

	// state is cleaned up after successful publish
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   posEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  nil,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	otherSigner, err := signing.NewEdSigner()
	require.NoError(t, err)
	posAtx := newAtx(challenge, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(otherSigner, posAtx))
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPosAtx))

	// generate and store initial post in state
	post := nipost.Post{
		Indices: types.RandomBytes(10),
		Nonce:   rand.Uint32(),
		Pow:     rand.Uint64(),

		NumUnits:      uint32(12),
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      types.VRFPostIndex(rand.Uint64()),
	}
	require.NoError(t, nipost.AddInitialPost(tab.localDb, sig.NodeID(), post))
	initialPost := &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}
	meta := &types.PostMetadata{
		Challenge:     shared.ZeroChallenge,
		LabelsPerUnit: tab.conf.LabelsPerUnit,
	}
	tab.mValidator.EXPECT().Post(gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, meta, post.NumUnits).
		Return(nil)

	// create and publish ATX
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	atx, err := publishAtx(t, tab, sig.NodeID(), posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx)

	// state is cleaned up
	_, err = nipost.Challenge(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBuilder_PublishActivationTx_NoPrevATX_PublishFails_InitialPost_preserved(t *testing.T) {
	tab := newTestBuilder(t, 1, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	sig := maps.Values(tab.signers)[0]

	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   posEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  nil,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	otherSigner, err := signing.NewEdSigner()
	require.NoError(t, err)
	posAtx := newAtx(challenge, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(otherSigner, posAtx))
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPosAtx))

	// generate and store initial refPost in state
	refPost := nipost.Post{
		Indices: types.RandomBytes(10),
		Nonce:   rand.Uint32(),
		Pow:     rand.Uint64(),

		NumUnits:      uint32(12),
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      types.VRFPostIndex(rand.Uint64()),
	}
	require.NoError(t, nipost.AddInitialPost(tab.localDb, sig.NodeID(), refPost))
	initialPost := &types.Post{
		Nonce:   refPost.Nonce,
		Indices: refPost.Indices,
		Pow:     refPost.Pow,
	}
	meta := &types.PostMetadata{
		Challenge:     shared.ZeroChallenge,
		LabelsPerUnit: tab.conf.LabelsPerUnit,
	}
	tab.mValidator.EXPECT().
		Post(gomock.Any(), sig.NodeID(), refPost.CommitmentATX, initialPost, meta, refPost.NumUnits).
		Return(nil)

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
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

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
	post, err := nipost.InitialPost(tab.localDB, sig.NodeID())
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

	initialPost := &types.Post{
		Nonce:   0,
		Indices: make([]byte, 10),
	}

	currentLayer := postGenesisEpoch.FirstLayer().Add(3)
	prevAtxPostEpoch := postGenesisEpoch
	postAtxPubEpoch := postGenesisEpoch

	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   postAtxPubEpoch,
		PositioningATX: tab.goldenATXID,
		CommitmentATX:  &tab.goldenATXID,
	}
	poetBytes := []byte("66666")
	nipostData := newNIPostWithPoet(t, poetBytes)
	posAtx := newAtx(challenge, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(otherSigner, posAtx))
	vPosAtx, err := posAtx.Verify(0, 2)
	r.NoError(err)
	r.NoError(atxs.Add(tab.db, vPosAtx))

	challenge = types.NIPostChallenge{
		Sequence:       0,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   prevAtxPostEpoch,
		PositioningATX: posAtx.ID(),
		CommitmentATX:  nil,
	}
	challenge.InitialPost = initialPost
	prevAtx := newAtx(challenge, nipostData.NIPost, 2, types.Address{})
	prevAtx.InitialPost = initialPost
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxs.Add(tab.db, vPrevAtx))

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
		func(_ context.Context, _ *signing.EdSigner, _ types.EpochID, _ types.Hash32) (*nipost.NIPostState, error) {
			currentLayer = currentLayer.Add(5)
			return newNIPostWithPoet(t, poetBytes), nil
		})

	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

	tab.mpub.EXPECT().
		Publish(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, msg []byte) error {
			atx, err := wire.ActivationTxFromBytes(msg)
			require.NoError(t, err)
			atx.SetReceived(time.Now().Local())

			atx.SetEffectiveNumUnits(atx.NumUnits)
			vAtx, err := atx.Verify(0, 1)
			r.NoError(err)
			r.Equal(sig.NodeID(), vAtx.SmesherID)

			r.NoError(atxs.Add(tab.db, vAtx))

			r.Equal(prevAtx.Sequence+1, atx.Sequence)
			r.Equal(prevAtx.ID(), atx.PrevATXID)
			r.Nil(atx.InitialPost)
			r.Nil(atx.CommitmentATX)
			r.Nil(atx.VRFNonce)

			r.Equal(posAtx.ID(), atx.PositioningATX)
			r.Equal(postAtxPubEpoch+1, atx.PublishEpoch)
			r.Equal(types.BytesToHash(poetBytes), atx.GetPoetProofRef())

			return nil
		})

	tab.mnipost.EXPECT().ResetState(sig.NodeID()).Return(nil)

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

	currentLayer := postGenesisEpoch.FirstLayer().Add(3)
	posEpoch := postGenesisEpoch
	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   posEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  &types.ATXID{4, 5, 6},
	}
	poetBytes := []byte("66666")
	nipostData := newNIPostWithPoet(t, poetBytes)
	posAtx := newAtx(challenge, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(otherSigner, posAtx))
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxs.Add(tab.db, vPosAtx))

	// Act & Assert
	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(closedChan).AnyTimes()

	tab.mclock.EXPECT().CurrentLayer().Return(currentLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(layer types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currentLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(layer))
		}).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(vPosAtx.PublishEpoch.FirstLayer().Add(layersPerEpoch)).DoAndReturn(
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
		func(_ context.Context, _ *signing.EdSigner, _ types.EpochID, _ types.Hash32) (*nipost.NIPostState, error) {
			currentLayer = currentLayer.Add(layersPerEpoch)
			return newNIPostWithPoet(t, poetBytes), nil
		})

	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	tab.mpub.EXPECT().
		Publish(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, msg []byte) error {
			atx, err := wire.ActivationTxFromBytes(msg)
			require.NoError(t, err)
			atx.SetReceived(time.Now().Local())

			atx.SetEffectiveNumUnits(atx.NumUnits)
			vAtx, err := atx.Verify(0, 1)
			r.NoError(err)
			r.Equal(sig.NodeID(), vAtx.SmesherID)

			r.NoError(atxs.Add(tab.db, vAtx))

			r.Zero(atx.Sequence)
			r.Equal(types.EmptyATXID, atx.PrevATXID)
			r.NotNil(atx.InitialPost)

			r.Equal(posAtx.ID(), atx.PositioningATX)
			r.Equal(posEpoch+1, atx.PublishEpoch)
			r.Equal(types.BytesToHash(poetBytes), atx.GetPoetProofRef())

			return nil
		})

	post := nipost.Post{
		Indices: types.RandomBytes(10),
		Nonce:   rand.Uint32(),
		Pow:     rand.Uint64(),

		NumUnits:      uint32(12),
		CommitmentATX: types.RandomATXID(),
		VRFNonce:      types.VRFPostIndex(rand.Uint64()),
	}
	require.NoError(t, nipost.AddInitialPost(tab.localDb, sig.NodeID(), post))
	initialPost := &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}
	meta := &types.PostMetadata{
		Challenge:     shared.ZeroChallenge,
		LabelsPerUnit: tab.conf.LabelsPerUnit,
	}
	tab.mValidator.EXPECT().Post(gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, meta, post.NumUnits).
		Return(nil)

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
	ch := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   posEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  nil,
	}
	nipostData := newNIPostWithPoet(t, []byte("66666"))
	posAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, posAtx))
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPosAtx))

	tab.mclock.EXPECT().CurrentLayer().Return(posEpoch.FirstLayer()).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	nipostErr := errors.New("NIPost builder error")
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), sig, gomock.Any(), gomock.Any()).Return(nil, nipostErr)
	tab.mValidator.EXPECT().VerifyChain(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
	require.ErrorIs(t, tab.PublishActivationTx(context.Background(), sig), nipostErr)

	// state is preserved
	challenge, err := nipost.Challenge(tab.localDB, sig.NodeID())
	require.NoError(t, err)
	require.NotNil(t, challenge)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nipost := newNIPostWithPoet(t, []byte("66666"))
	coinbase := types.Address{4, 5, 6}
	atx := newActivationTx(
		t,
		sig,
		1,
		types.ATXID{1, 2, 3},
		types.ATXID{1, 2, 3},
		nil,
		types.EpochID(5),
		1,
		100,
		coinbase,
		100,
		nipost.NIPost,
	)

	act := newActivationTx(t,
		sig,
		2,
		atx.ID(),
		atx.ID(),
		nil,
		atx.PublishEpoch.Add(10),
		0,
		100,
		coinbase,
		100,
		nipost.NIPost,
	)

	bt, err := codec.Encode(wire.ActivationTxToWireV1(act.ActivationTx))
	require.NoError(t, err)

	a, err := wire.ActivationTxFromBytes(bt)
	require.NoError(t, err)

	bt2, err := codec.Encode(wire.ActivationTxToWireV1(a))
	require.NoError(t, err)

	require.Equal(t, bt, bt2)
}

func TestBuilder_SignAtx(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      prevAtx,
		PublishEpoch:   types.EpochID(15),
		PositioningATX: prevAtx,
		CommitmentATX:  nil,
	}
	nipost := newNIPostWithPoet(t, []byte("66666"))
	atx := newAtx(challenge, nipost.NIPost, 100, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, atx))

	msg := wire.ActivationTxToWireV1(atx).SignedBytes()
	ok := signing.NewEdVerifier().Verify(signing.ATX, sig.NodeID(), msg, atx.Signature)
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
	posEpoch := types.EpochID(0)
	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.ATXID{1, 2, 3},
		PublishEpoch:   posEpoch,
		PositioningATX: types.ATXID{1, 2, 3},
		CommitmentATX:  nil,
	}
	poetBytes := []byte("66666")
	nipostData := newNIPostWithPoet(t, poetBytes)
	prevAtx := newAtx(challenge, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx))

	publishEpoch := posEpoch + 1
	currLayer := posEpoch.FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
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
			func(_ context.Context, _ *signing.EdSigner, _ types.EpochID, _ types.Hash32) (*nipost.NIPostState, error) {
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
				return newNIPostWithPoet(t, poetBytes), nil
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

	var atx types.ActivationTx
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(ctx context.Context, s string, b []byte) error {
			decoded, err := wire.ActivationTxFromBytes(b)
			require.NoError(t, err)
			atx = *decoded
			atx.SetReceived(time.Now().Local())

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
		require.Equal(t, atx.ID(), types.BytesToATXID(atxEvent.GetId()))
	case <-time.After(5 * time.Second):
		require.FailNow(t, "timed out waiting for activation event")
	}

	// state is cleaned up
	_, err = nipost.InitialPost(tab.localDB, sig.NodeID())
	require.ErrorIs(t, err, sql.ErrNotFound)

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
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: post.CommitmentATX,
			Nonce:         &post.VRFNonce,

			NumUnits:      post.NumUnits,
			LabelsPerUnit: tab.conf.LabelsPerUnit,
		},
		nil,
	)
	meta := &types.PostMetadata{
		Challenge:     shared.ZeroChallenge,
		LabelsPerUnit: tab.conf.LabelsPerUnit,
	}
	tab.mValidator.EXPECT().Post(gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, meta, post.NumUnits).
		Return(nil)
	require.NoError(t, tab.buildInitialPost(context.Background(), sig.NodeID()))

	posEpoch := postGenesisEpoch + 1
	challenge := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   posEpoch,
		PositioningATX: tab.goldenATXID,
		CommitmentATX:  &tab.goldenATXID,
	}
	poetByte := []byte("66666")
	nipost := newNIPostWithPoet(t, poetByte)
	prevAtx := newAtx(challenge, nipost.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sig, prevAtx))
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vPrevAtx))

	currLayer := posEpoch.FirstLayer().Add(1)
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	atx, err := publishAtx(t, tab, sig.NodeID(), posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx)

	require.Equal(t, sig.NodeID(), atx.SmesherID)
	require.Equal(t, vPrevAtx.Sequence+1, atx.Sequence)
	require.Equal(t, vPrevAtx.ID(), atx.PrevATXID)
	require.Nil(t, atx.InitialPost)
	require.Nil(t, atx.VRFNonce)
	require.Equal(t, vPrevAtx.ID(), atx.PositioningATX)
	require.Equal(t, vPrevAtx.PublishEpoch+1, atx.PublishEpoch)
	require.Equal(t, types.BytesToHash(poetByte), atx.GetPoetProofRef())

	// postClient.Proof() should not be called again
	require.NoError(t, tab.buildInitialPost(context.Background(), sig.NodeID()))
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
	meta := &types.PostMetadata{
		Challenge:     shared.ZeroChallenge,
		LabelsPerUnit: tab.conf.LabelsPerUnit,
	}
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: commitmentATX,
			Nonce:         &nonce,

			NumUnits:      numUnits,
			LabelsPerUnit: tab.conf.LabelsPerUnit,
		},
		nil,
	)
	tab.mValidator.EXPECT().Post(gomock.Any(), sig.NodeID(), commitmentATX, initialPost, meta, numUnits).
		Return(nil)
	require.NoError(t, tab.buildInitialPost(context.Background(), sig.NodeID()))

	// postClient.Proof() should not be called again
	require.NoError(t, tab.buildInitialPost(context.Background(), sig.NodeID()))
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
	meta := &types.PostMetadata{
		Challenge:     shared.ZeroChallenge,
		LabelsPerUnit: tab.conf.LabelsPerUnit,
	}
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: commitmentATX,

			NumUnits:      numUnits,
			LabelsPerUnit: tab.conf.LabelsPerUnit,
		},
		nil,
	)
	tab.mValidator.EXPECT().Post(gomock.Any(), sig.NodeID(), commitmentATX, initialPost, meta, numUnits).
		Return(nil)
	require.ErrorContains(t, tab.buildInitialPost(context.Background(), sig.NodeID()), "nil VRF nonce")

	observedLogs := tab.observedLogs.FilterLevelExact(zapcore.ErrorLevel)
	require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
	require.Equal(t, zapcore.ErrorLevel, observedLogs.All()[0].Level)
	require.Equal(t, "initial PoST is invalid: missing VRF nonce. Check your PoST data", observedLogs.All()[0].Message)
	require.Equal(t, sig.NodeID().ShortString(), observedLogs.All()[0].ContextMap()["smesherID"])

	// postClient.Proof() should be called again and no error if vrf nonce is provided
	nonce := types.VRFPostIndex(rand.Uint64())
	tab.mnipost.EXPECT().Proof(gomock.Any(), sig.NodeID(), shared.ZeroChallenge).Return(
		initialPost,
		&types.PostInfo{
			NodeID:        sig.NodeID(),
			CommitmentATX: commitmentATX,
			Nonce:         &nonce,

			NumUnits:      numUnits,
			LabelsPerUnit: tab.conf.LabelsPerUnit,
		},
		nil,
	)
	require.NoError(t, tab.buildInitialPost(context.Background(), sig.NodeID()))
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
			tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(&nipost.NIPostState{}, nil)
			closed := make(chan struct{})
			close(closed)
			tab.mclock.EXPECT().AwaitLayer(types.EpochID(1).FirstLayer()).Return(closed).AnyTimes()
			tab.mclock.EXPECT().AwaitLayer(types.EpochID(2).FirstLayer()).Return(closed).AnyTimes()
			tab.mpub.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ string, got []byte) error {
					gotAtx, err := wire.ActivationTxFromBytes(got)
					require.NoError(t, err)
					require.Equal(t, tc.targetEpoch, gotAtx.TargetEpoch())
					return nil
				})

			post := nipost.Post{
				Indices: types.RandomBytes(10),
				Nonce:   rand.Uint32(),
				Pow:     rand.Uint64(),

				NumUnits:      uint32(12),
				CommitmentATX: types.RandomATXID(),
				VRFNonce:      types.VRFPostIndex(rand.Uint64()),
			}
			require.NoError(t, nipost.AddInitialPost(tab.localDb, sig.NodeID(), post))
			initialPost := &types.Post{
				Nonce:   post.Nonce,
				Indices: post.Indices,
				Pow:     post.Pow,
			}
			meta := &types.PostMetadata{
				Challenge:     shared.ZeroChallenge,
				LabelsPerUnit: tab.conf.LabelsPerUnit,
			}
			tab.mValidator.EXPECT().Post(
				gomock.Any(), sig.NodeID(), post.CommitmentATX, initialPost, meta, post.NumUnits,
			).Return(nil)

			require.NoError(t, tab.PublishActivationTx(context.Background(), sig))
		})
	}
}

func TestWaitingToBuildNipostChallengeWithJitter(t *testing.T) {
	t.Run("before grace period", func(t *testing.T) {
		//          ┌──grace period──┐
		//          │                │
		// ───▲─────|──────|─────────|----> time
		//    │     └jitter|         └round start
		//   now
		deadline := buildNipostChallengeStartDeadline(time.Now().Add(2*time.Hour), time.Hour)
		require.Greater(t, deadline, time.Now().Add(time.Hour))
		require.LessOrEqual(t, deadline, time.Now().Add(time.Hour+time.Second*36))
	})
	t.Run("after grace period, within max jitter value", func(t *testing.T) {
		//          ┌──grace period──┐
		//          │                │
		// ─────────|──▲────|────────|----> time
		//          └ji│tter|        └round start
		//            now
		deadline := buildNipostChallengeStartDeadline(time.Now().Add(time.Hour-time.Second*10), time.Hour)
		require.GreaterOrEqual(t, deadline, time.Now().Add(-time.Second*10))
		// jitter is 1% = 36s for 1h grace period
		require.LessOrEqual(t, deadline, time.Now().Add(time.Second*(36-10)))
	})
	t.Run("after jitter max value", func(t *testing.T) {
		//          ┌──grace period──┐
		//          │                │
		// ─────────|──────|──▲──────|----> time
		//          └jitter|  │       └round start
		//                   now
		deadline := buildNipostChallengeStartDeadline(time.Now().Add(time.Hour-time.Second*37), time.Hour)
		require.Less(t, deadline, time.Now())
	})
}

// Test if GetPositioningAtx disregards ATXs with invalid POST in their chain.
// It should pick an ATX with valid POST even though it's a lower height.
func TestGetPositioningAtxPicksAtxWithValidChain(t *testing.T) {
	tab := newTestBuilder(t, 1)
	sig := maps.Values(tab.signers)[0]

	// Invalid chain with high height
	sigInvalid, err := signing.NewEdSigner()
	require.NoError(t, err)
	ch := types.NIPostChallenge{
		Sequence:       1,
		PrevATXID:      types.EmptyATXID,
		PublishEpoch:   postGenesisEpoch,
		PositioningATX: tab.goldenATXID,
		CommitmentATX:  &tab.goldenATXID,
	}
	nipostData := newNIPostWithPoet(t, []byte("0"))
	invalidAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sigInvalid, invalidAtx))
	vInvalidAtx, err := invalidAtx.Verify(0, 100)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vInvalidAtx))

	// Valid chain with lower height
	sigValid, err := signing.NewEdSigner()
	require.NoError(t, err)
	nipostData = newNIPostWithPoet(t, []byte("1"))
	validAtx := newAtx(ch, nipostData.NIPost, 2, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(sigValid, validAtx))
	vValidAtx, err := validAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.db, vValidAtx))

	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), invalidAtx.ID(), tab.goldenATXID, gomock.Any()).
		Return(errors.New(""))
	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), validAtx.ID(), tab.goldenATXID, gomock.Any())

	posAtxID, err := tab.getPositioningAtx(context.Background(), sig.NodeID(), 77)
	require.NoError(t, err)
	require.Equal(t, posAtxID, vValidAtx.ID())

	// should use the cached positioning ATX when asked for the same publish epoch
	posAtxID, err = tab.getPositioningAtx(context.Background(), sig.NodeID(), 77)
	require.NoError(t, err)
	require.Equal(t, posAtxID, vValidAtx.ID())

	// should lookup again when asked for a different publish epoch
	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), invalidAtx.ID(), tab.goldenATXID, gomock.Any()).
		Return(errors.New(""))
	tab.mValidator.EXPECT().
		VerifyChain(gomock.Any(), validAtx.ID(), tab.goldenATXID, gomock.Any())

	posAtxID, err = tab.getPositioningAtx(context.Background(), sig.NodeID(), 99)
	require.NoError(t, err)
	require.Equal(t, posAtxID, vValidAtx.ID())
}

func TestGetPositioningAtxDbFailed(t *testing.T) {
	tab := newTestBuilder(t, 1)
	sig := maps.Values(tab.signers)[0]

	db := sqlmocks.NewMockExecutor(gomock.NewController(t))
	tab.Builder.db = db
	expected := errors.New("db error")
	db.EXPECT().Exec(gomock.Any(), gomock.Any(), gomock.Any()).Return(0, expected)

	none, err := tab.getPositioningAtx(context.Background(), sig.NodeID(), 99)
	require.ErrorIs(t, err, expected)
	require.Equal(t, types.ATXID{}, none)
}
