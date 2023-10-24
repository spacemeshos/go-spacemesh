package activation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
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

func newChallenge(sequence uint64, prevAtxID, posAtxID types.ATXID, PublishEpoch types.EpochID, cATX *types.ATXID) types.NIPostChallenge {
	return types.NIPostChallenge{
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PublishEpoch:   PublishEpoch,
		PositioningATX: posAtxID,
		CommitmentATX:  cATX,
	}
}

func newAtx(t testing.TB, sig *signing.EdSigner, challenge types.NIPostChallenge, nipost *types.NIPost, numUnits uint32, coinbase types.Address) *types.ActivationTx {
	atx := types.NewActivationTx(challenge, coinbase, nipost, numUnits, nil)
	atx.SetEffectiveNumUnits(numUnits)
	atx.SetReceived(time.Now())
	return atx
}

func newActivationTx(
	t testing.TB,
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
) *types.VerifiedActivationTx {
	challenge := newChallenge(sequence, prevATX, positioningATX, publishEpoch, cATX)
	atx := newAtx(t, sig, challenge, nipost, numUnits, coinbase)
	if sequence == 0 {
		nodeID := sig.NodeID()
		atx.NodeID = &nodeID
	}

	atx.SetEffectiveNumUnits(numUnits)
	atx.SetReceived(time.Now())
	require.NoError(t, SignAndFinalizeAtx(sig, atx))
	vAtx, err := atx.Verify(startTick, numTicks)
	require.NoError(t, err)
	return vAtx
}

type testAtxBuilder struct {
	*Builder
	cdb         *datastore.CachedDB
	sig         *signing.EdSigner
	nodeID      types.NodeID
	coinbase    types.Address
	goldenATXID types.ATXID

	mpub        *mocks.MockPublisher
	mpostSvc    *MockpostService
	mnipost     *MocknipostBuilder
	mpostClient *MockPostClient
	mclock      *MocklayerClock
	msync       *Mocksyncer
	mValidator  *MocknipostValidator
}

func newTestBuilder(tb testing.TB, opts ...BuilderOption) *testAtxBuilder {
	lg := logtest.New(tb)
	edSigner, err := signing.NewEdSigner()
	require.NoError(tb, err)
	ctrl := gomock.NewController(tb)
	tab := &testAtxBuilder{
		cdb:         datastore.NewCachedDB(sql.InMemory(), lg),
		sig:         edSigner,
		nodeID:      edSigner.NodeID(),
		coinbase:    types.GenerateAddress([]byte("33333")),
		goldenATXID: types.ATXID(types.HexToHash32("77777")),
		mpub:        mocks.NewMockPublisher(ctrl),
		mpostSvc:    NewMockpostService(ctrl),
		mnipost:     NewMocknipostBuilder(ctrl),
		mpostClient: NewMockPostClient(ctrl),
		mclock:      NewMocklayerClock(ctrl),
		msync:       NewMocksyncer(ctrl),
		mValidator:  NewMocknipostValidator(ctrl),
	}

	opts = append(opts, WithValidator(tab.mValidator))

	cfg := Config{
		CoinbaseAccount: tab.coinbase,
		GoldenATXID:     tab.goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).AnyTimes()
	tab.mpostSvc.EXPECT().Client(tab.nodeID).Return(tab.mpostClient, nil).AnyTimes()

	b := NewBuilder(cfg, tab.nodeID, tab.sig, tab.cdb, tab.mpub, tab.mpostSvc, tab.mnipost, tab.mclock, tab.msync, lg, opts...)
	b.initialPost = &types.Post{
		Nonce:   0,
		Indices: make([]byte, 10),
	}
	tab.Builder = b
	dir := tb.TempDir()
	tab.mnipost.EXPECT().DataDir().Return(dir).AnyTimes()
	return tab
}

func assertLastAtx(r *require.Assertions, nodeID types.NodeID, poetRef types.Hash32, newAtx *types.ActivationTx, posAtx, prevAtx *types.VerifiedActivationTx, layersPerEpoch uint32) {
	atx := newAtx
	r.Equal(nodeID, atx.SmesherID)
	if prevAtx != nil {
		r.Equal(prevAtx.Sequence+1, atx.Sequence)
		r.Equal(prevAtx.ID(), atx.PrevATXID)
		r.Nil(atx.InitialPost)
		r.Nil(atx.VRFNonce)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(types.EmptyATXID, atx.PrevATXID)
		r.NotNil(atx.InitialPost)
		r.NotNil(atx.VRFNonce)
	}
	r.Equal(posAtx.ID(), atx.PositioningATX)
	r.Equal(posAtx.PublishEpoch+1, atx.PublishEpoch)
	r.Equal(poetRef, atx.GetPoetProofRef())
}

func publishAtx(
	t *testing.T,
	tab *testAtxBuilder,
	posEpoch types.EpochID,
	currLayer *types.LayerID, // pointer to keep current layer consistent across calls
	buildNIPostLayerDuration uint32,
) (*types.ActivationTx, error) {
	t.Helper()

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
		NodeID:        tab.nodeID,
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: layersPerEpoch,
	}, nil).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error) {
			*currLayer = currLayer.Add(buildNIPostLayerDuration)
			return newNIPostWithChallenge(t, challenge.Hash(), []byte("66666")), nil
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
			var gotAtx types.ActivationTx
			require.NoError(t, codec.Decode(got, &gotAtx))
			gotAtx.SetReceived(time.Now().Local())
			built = &gotAtx
			require.NoError(t, built.Initialize())
			built.SetEffectiveNumUnits(gotAtx.NumUnits)
			vatx, err := built.Verify(0, 1)
			require.NoError(t, err)
			require.NoError(t, atxs.Add(tab.cdb, vatx))
			return nil
		})
	// create and publish ATX
	err := tab.PublishActivationTx(context.Background())
	return built, err
}

func addPrevAtx(t *testing.T, db sql.Executor, epoch types.EpochID, sig *signing.EdSigner) *types.VerifiedActivationTx {
	challenge := types.NIPostChallenge{
		PublishEpoch: epoch,
	}
	atx := types.NewActivationTx(challenge, types.Address{}, nil, 2, nil)
	atx.SetEffectiveNumUnits(2)
	return addAtx(t, db, sig, atx)
}

func addAtx(t *testing.T, db sql.Executor, sig *signing.EdSigner, atx *types.ActivationTx) *types.VerifiedActivationTx {
	require.NoError(t, SignAndFinalizeAtx(sig, atx))
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, vAtx))
	return vAtx
}

// ========== Tests ==========

func TestBuilder_StartSmeshingCoinbase(t *testing.T) {
	tab := newTestBuilder(t)
	coinbase := types.Address{1, 1, 1}

	tab.mpostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, b []byte) (*types.Post, *types.PostInfo, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}).AnyTimes()
	tab.mValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()
	require.NoError(t, tab.StartSmeshing(coinbase))
	require.Equal(t, coinbase, tab.Coinbase())

	// calling StartSmeshing more than once before calling StopSmeshing is an error
	require.ErrorContains(t, tab.StartSmeshing(coinbase), "already started")

	require.NoError(t, tab.StopSmeshing(true))
}

func TestBuilder_RestartSmeshing(t *testing.T) {
	now := time.Now()
	getBuilder := func(t *testing.T) *Builder {
		tab := newTestBuilder(t)
		tab.mpostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).AnyTimes().Return(&types.Post{}, &types.PostMetadata{
			Challenge: shared.ZeroChallenge,
		}, nil)
		tab.mValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		ch := make(chan struct{})
		close(ch)
		tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(ch).AnyTimes()
		tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
		tab.mclock.EXPECT().LayerToTime(gomock.Any()).Return(now).AnyTimes()
		return tab.Builder
	}

	t.Run("Single threaded", func(t *testing.T) {
		builder := getBuilder(t)
		for i := 0; i < 100; i++ {
			require.NoError(t, builder.StartSmeshing(types.Address{}))
			require.Never(t, func() bool { return !builder.Smeshing() }, 400*time.Microsecond, 50*time.Microsecond, "failed on execution %d", i)
			require.NoError(t, builder.StopSmeshing(true))
			require.Eventually(t, func() bool { return !builder.Smeshing() }, 100*time.Millisecond, time.Millisecond, "failed on execution %d", i)
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
				for i := 0; i < 100; i++ {
					builder.StartSmeshing(types.Address{})
					builder.StopSmeshing(true)
				}
				return nil
			})
		}
		require.NoError(t, eg.Wait())
	})
}

func TestBuilder_StopSmeshing_Delete(t *testing.T) {
	tab := newTestBuilder(t)

	currLayer := (postGenesisEpoch + 1).FirstLayer()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ *types.NIPostChallenge) (*types.NIPost, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}).AnyTimes()
	tab.mpostClient.EXPECT().Proof(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, b []byte) (*types.Post, *types.PostInfo, error) {
		<-ctx.Done()
		return nil, nil, ctx.Err()
	}).AnyTimes()

	// Create state files
	require.NoError(t, saveBuilderState(tab.nipostBuilder.DataDir(), &types.NIPostBuilderState{}))
	require.NoError(t, savePost(tab.nipostBuilder.DataDir(), &types.Post{}))
	require.NoError(t, SaveNipostChallenge(tab.nipostBuilder.DataDir(), &types.NIPostChallenge{PublishEpoch: postGenesisEpoch + 2}))
	files, err := os.ReadDir(tab.nipostBuilder.DataDir())
	require.NoError(t, err)
	require.Len(t, files, 3) // 3 state files created

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(false))
	files, err = os.ReadDir(tab.nipostBuilder.DataDir())
	require.NoError(t, err)
	require.Len(t, files, 3) // state files still present

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true))
	files, err = os.ReadDir(tab.nipostBuilder.DataDir())
	require.NoError(t, err)
	require.Len(t, files, 0) // state files deleted

	require.NoError(t, tab.StartSmeshing(types.Address{}))
	require.NoError(t, tab.StopSmeshing(true)) // no-op
	files, err = os.ReadDir(tab.nipostBuilder.DataDir())
	require.NoError(t, err)
	require.Len(t, files, 0) // state files still deleted
}

func TestBuilder_StopSmeshing_failsWhenNotStarted(t *testing.T) {
	tab := newTestBuilder(t)
	require.ErrorContains(t, tab.StopSmeshing(true), "not started")
}

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration}))
	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	// create and publish ATX
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(5)
	atx1, err := publishAtx(t, tab, posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx1)

	// create and publish another ATX
	currLayer = (posEpoch + 1).FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).Times(5)
	atx2, err := publishAtx(t, tab, atx1.PublishEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotEqual(t, atx1, atx2)
	require.Equal(t, atx1.TargetEpoch()+1, atx2.TargetEpoch())
}

// TestBuilder_Loop_WaitsOnStaleChallenge checks if loop waits between attempts
// failing with ErrATXChallengeExpired.
func TestBuilder_Loop_WaitsOnStaleChallenge(t *testing.T) {
	// Arrange
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	// current layer is too late to be able to build a nipost on time
	currLayer := (postGenesisEpoch + 1).FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, postGenesisEpoch, nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).Return(nil, ErrATXChallengeExpired)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tab.mclock.EXPECT().AwaitLayer(currLayer.Add(1)).Do(func(got types.LayerID) <-chan struct{} {
		cancel()
		ch := make(chan struct{})
		close(ch)
		return ch
	})

	// Act & Verify
	var eg errgroup.Group
	eg.Go(func() error {
		tab.run(ctx)
		return nil
	})

	require.NoError(t, eg.Wait())
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posEpoch := postGenesisEpoch
	currLayer := postGenesisEpoch.FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, postGenesisEpoch, nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

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
		NodeID:        tab.nodeID,
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: layersPerEpoch,
	}, nil).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(t, challenge.Hash(), []byte("66666")), nil
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
	publishErr := errors.New("blah")
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			var gotAtx types.ActivationTx
			require.NoError(t, codec.Decode(got, &gotAtx))
			gotAtx.SetReceived(time.Now().Local())
			built = &gotAtx
			require.NoError(t, built.Initialize())
			return publishErr
		})
	// create and publish ATX
	err = tab.PublishActivationTx(context.Background())
	require.ErrorIs(t, err, publishErr)
	require.NotNil(t, built)

	// now causing it to publish again, it should use the same atx
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			var gotAtx types.ActivationTx
			require.NoError(t, codec.Decode(got, &gotAtx))
			gotAtx.SetReceived(time.Now().Local())
			require.NoError(t, gotAtx.Initialize())
			built.SetReceived(gotAtx.Received())
			require.Equal(t, &gotAtx, built)
			return nil
		})
	require.NoError(t, tab.PublishActivationTx(context.Background()))

	// if the network works and we try to publish a new ATX, the timeout should result in a clean state (so a NIPost should be built)
	posEpoch = posEpoch + 1
	challenge = newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	built2, err := publishAtx(t, tab, posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPostChallenge, built2.NIPostChallenge)
	require.Equal(t, built.TargetEpoch()+1, built2.TargetEpoch())
}

func TestBuilder_PublishActivationTx_RebuildNIPostWhenTargetEpochPassed(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posEpoch := types.EpochID(2)
	currLayer := posEpoch.FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

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
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{}, nil)
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(t, challenge.Hash(), []byte("66666")), nil
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
	publishErr := errors.New("blah")
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			var gotAtx types.ActivationTx
			require.NoError(t, codec.Decode(got, &gotAtx))
			gotAtx.SetReceived(time.Now().Local())
			built = &gotAtx
			require.NoError(t, built.Initialize())
			return publishErr
		})
	// create and publish ATX
	err = tab.PublishActivationTx(context.Background())
	require.ErrorIs(t, err, publishErr)
	require.NotNil(t, built)

	// We started building the NIPost in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the target epoch (4) has passed, so we'll start the ATX builder in epoch 5 and ensure it discards
	// a stale challenge and builds a new NIPost.
	posEpoch = types.EpochID(5)
	currLayer = posEpoch.FirstLayer()
	challenge = newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	built2, err := publishAtx(t, tab, posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPostChallenge, built2.NIPostChallenge)
	require.Equal(t, built.TargetEpoch()+3, built2.TargetEpoch())
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	otherSigner, err := signing.NewEdSigner()
	require.NoError(t, err)
	posAtx := newAtx(t, otherSigner, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(otherSigner, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))

	// create and publish ATX
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	atx, err := publishAtx(t, tab, posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx)
}

func TestBuilder_PublishActivationTx_PrevATXWithoutPrevATX(t *testing.T) {
	r := require.New(t)

	// Arrange
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	otherSigner, err := signing.NewEdSigner()
	r.NoError(err)

	initialPost := &types.Post{
		Nonce:   0,
		Indices: make([]byte, 10),
	}

	currentLayer := postGenesisEpoch.FirstLayer().Add(3)
	prevAtxPostEpoch := postGenesisEpoch
	postAtxPubEpoch := postGenesisEpoch

	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, postAtxPubEpoch, nil)
	poetBytes := []byte("66666")
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), poetBytes)
	posAtx := newAtx(t, otherSigner, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(otherSigner, posAtx)
	vPosAtx, err := posAtx.Verify(0, 2)
	r.NoError(err)
	r.NoError(atxs.Add(tab.cdb, vPosAtx))

	challenge = newChallenge(0, types.EmptyATXID, posAtx.ID(), prevAtxPostEpoch, nil)
	challenge.InitialPost = initialPost
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	prevAtx.InitialPost = initialPost
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxs.Add(tab.cdb, vPrevAtx))

	// Act
	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).AnyTimes()

	tab.mclock.EXPECT().CurrentLayer().Return(currentLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(layer types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currentLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(layer))
		}).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(vPosAtx.PublishEpoch.FirstLayer().Add(layersPerEpoch)).DoAndReturn(func(layer types.LayerID) <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).Times(1)

	nonce := types.VRFPostIndex(123)
	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        tab.nodeID,
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: layersPerEpoch,
	}, nil).AnyTimes()

	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error) {
			currentLayer = currentLayer.Add(5)
			return newNIPostWithChallenge(t, challenge.Hash(), poetBytes), nil
		})

	tab.mpub.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ string, msg []byte) error {
		var atx types.ActivationTx
		require.NoError(t, codec.Decode(msg, &atx))
		atx.SetReceived(time.Now().Local())

		atx.SetEffectiveNumUnits(atx.NumUnits)
		vAtx, err := atx.Verify(0, 1)
		r.NoError(err)
		r.Equal(tab.nodeID, vAtx.SmesherID)

		r.NoError(atxs.Add(tab.cdb, vAtx))

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

	r.NoError(tab.PublishActivationTx(context.Background()))
}

func TestBuilder_PublishActivationTx_TargetsEpochBasedOnPosAtx(t *testing.T) {
	r := require.New(t)

	// Arrange
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	otherSigner, err := signing.NewEdSigner()
	r.NoError(err)

	currentLayer := postGenesisEpoch.FirstLayer().Add(3)
	posEpoch := postGenesisEpoch
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	poetBytes := []byte("66666")
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), poetBytes)
	posAtx := newAtx(t, otherSigner, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(otherSigner, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxs.Add(tab.cdb, vPosAtx))

	// Act & Assert
	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).AnyTimes()

	tab.mclock.EXPECT().CurrentLayer().Return(currentLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(layer types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currentLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(layer))
		}).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(vPosAtx.PublishEpoch.FirstLayer().Add(layersPerEpoch)).DoAndReturn(func(types.LayerID) <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).Times(1)

	nonce := types.VRFPostIndex(123)
	commitmentATX := types.RandomATXID()
	tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{
		NodeID:        tab.nodeID,
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: layersPerEpoch,
	}, nil).AnyTimes()

	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error) {
			currentLayer = currentLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(t, challenge.Hash(), poetBytes), nil
		})

	tab.mpub.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ string, msg []byte) error {
		var atx types.ActivationTx
		require.NoError(t, codec.Decode(msg, &atx))
		atx.SetReceived(time.Now().Local())

		atx.SetEffectiveNumUnits(atx.NumUnits)
		vAtx, err := atx.Verify(0, 1)
		r.NoError(err)
		r.Equal(tab.nodeID, vAtx.SmesherID)

		r.NoError(atxs.Add(tab.cdb, vAtx))

		r.Zero(atx.Sequence)
		r.Equal(types.EmptyATXID, atx.PrevATXID)
		r.NotNil(atx.InitialPost)

		r.Equal(posAtx.ID(), atx.PositioningATX)
		r.Equal(posEpoch+1, atx.PublishEpoch)
		r.Equal(types.BytesToHash(poetBytes), atx.GetPoetProofRef())

		return nil
	})

	r.NoError(tab.PublishActivationTx(context.Background()))
}

func TestBuilder_PublishActivationTx_FailsWhenNIPostBuilderFails(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))

	tab.mclock.EXPECT().CurrentLayer().Return(posEpoch.FirstLayer()).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	nipostErr := fmt.Errorf("NIPost builder error")
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).Return(nil, nipostErr)
	require.ErrorIs(t, tab.PublishActivationTx(context.Background()), nipostErr)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	coinbase := types.Address{4, 5, 6}
	atx := newActivationTx(t, sig, 1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, nil, types.EpochID(5), 1, 100, coinbase, 100, nipost)
	require.NoError(t, atxs.Add(cdb, atx))

	act := newActivationTx(t, sig, 2, atx.ID(), atx.ID(), nil, atx.PublishEpoch.Add(10), 0, 100, coinbase, 100, nipost)

	bt, err := codec.Encode(act)
	require.NoError(t, err)

	var a types.ActivationTx
	require.NoError(t, codec.Decode(bt, &a))
	a.SetReceived(time.Now().Local())

	bt2, err := codec.Encode(&a)
	require.NoError(t, err)

	require.Equal(t, bt, bt2)
}

func TestBuilder_SignAtx(t *testing.T) {
	tab := newTestBuilder(t)
	prevAtx := types.ATXID(types.HexToHash32("0x111"))
	challenge := newChallenge(1, prevAtx, prevAtx, types.EpochID(15), nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	atx := newAtx(t, tab.sig, challenge, nipost, 100, types.Address{})
	require.NoError(t, SignAndFinalizeAtx(tab.signer, atx))

	ok := signing.NewEdVerifier().Verify(signing.ATX, tab.nodeID, atx.SignedBytes(), atx.Signature)
	require.True(t, ok)
	require.Equal(t, tab.nodeID, atx.SmesherID)
}

func TestBuilder_NIPostPublishRecovery(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posEpoch := postGenesisEpoch
	currLayer := posEpoch.FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

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
		NodeID:        tab.nodeID,
		CommitmentATX: commitmentATX,
		Nonce:         &nonce,

		NumUnits:      DefaultPostSetupOpts().NumUnits,
		LabelsPerUnit: layersPerEpoch,
	}, nil).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(t, challenge.Hash(), []byte("66666")), nil
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
	publishErr := errors.New("blah")
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			var gotAtx types.ActivationTx
			require.NoError(t, codec.Decode(got, &gotAtx))
			gotAtx.SetReceived(time.Now().Local())
			built = &gotAtx
			require.NoError(t, built.Initialize())
			return publishErr
		})
	// create and publish ATX
	err = tab.PublishActivationTx(context.Background())
	require.ErrorIs(t, err, publishErr)
	require.NotNil(t, built)

	// the challenge remains
	got, err := LoadNipostChallenge(tab.nipostBuilder.DataDir())
	require.NoError(t, err)
	require.NotEmpty(t, got)

	// now causing it to publish again, it should use the same atx
	tab.mpub.EXPECT().Publish(gomock.Any(), pubsub.AtxProtocol, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, got []byte) error {
			var gotAtx types.ActivationTx
			require.NoError(t, codec.Decode(got, &gotAtx))
			gotAtx.SetReceived(time.Now().Local())
			require.NoError(t, gotAtx.Initialize())
			built.SetReceived(gotAtx.Received())
			require.Equal(t, &gotAtx, built)
			return nil
		})
	require.NoError(t, tab.PublishActivationTx(context.Background()))
	got, err = LoadNipostChallenge(tab.nipostBuilder.DataDir())
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, got)

	posEpoch = posEpoch + 1
	challenge = newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	built2, err := publishAtx(t, tab, posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPostChallenge, built2.NIPostChallenge)
	require.Equal(t, built.TargetEpoch()+1, built2.TargetEpoch())

	got, err = LoadNipostChallenge(tab.nipostBuilder.DataDir())
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Empty(t, got)
}

func TestBuilder_RetryPublishActivationTx(t *testing.T) {
	retryInterval := 50 * time.Microsecond
	genesis := time.Now()
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: 150 * time.Millisecond}), WithPoetRetryInterval(retryInterval))
	tab.log = logtest.New(t, zap.InfoLevel)
	posEpoch := types.EpochID(0)
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	poetBytes := []byte("66666")
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), poetBytes)
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	currLayer := posEpoch.FirstLayer()
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()

	expectedTries := 3
	tries := 0
	var last time.Time
	builderConfirmation := make(chan struct{})
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).Times(expectedTries).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, error) {
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
			return newNIPostWithChallenge(t, challenge.Hash(), poetBytes), nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		tab.run(ctx)
		return nil
	})
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, eg.Wait())
	})

	select {
	case <-builderConfirmation:
	case <-time.After(5 * time.Second):
		require.FailNowf(t, "failed waiting for required number of tries", "only tried %d times", tries)
	}
}

func TestBuilder_InitialProofGeneratedOnce(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	tab.mpostClient.EXPECT().Proof(gomock.Any(), shared.ZeroChallenge).Return(&types.Post{}, &types.PostInfo{}, nil)
	tab.mValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	require.NoError(t, tab.generateInitialPost(context.Background()))

	posEpoch := postGenesisEpoch + 1
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posEpoch, nil)
	poetByte := []byte("66666")
	nipost := newNIPostWithChallenge(t, types.HexToHash32("55555"), poetByte)
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	currLayer := posEpoch.FirstLayer().Add(1)
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	atx, err := publishAtx(t, tab, posEpoch, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx)
	assertLastAtx(require.New(t), tab.nodeID, types.BytesToHash(poetByte), atx, vPrevAtx, vPrevAtx, layersPerEpoch)

	// GenerateProof() should not be called again
	require.NoError(t, tab.generateInitialPost(context.Background()))
}

func TestBuilder_InitialPostIsPersisted(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	tab.mpostClient.EXPECT().Proof(gomock.Any(), shared.ZeroChallenge).Return(&types.Post{}, &types.PostInfo{
		Challenge: shared.ZeroChallenge,
	}, nil)
	tab.mValidator.EXPECT().Post(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	require.NoError(t, tab.generateInitialPost(context.Background()))

	// GenerateProof() should not be called again
	require.NoError(t, tab.generateInitialPost(context.Background()))

	// Remove the persisted post file and try again
	require.NoError(t, os.Remove(filepath.Join(tab.nipostBuilder.DataDir(), postFilename)))
	tab.mpostClient.EXPECT().Proof(gomock.Any(), shared.ZeroChallenge).Return(&types.Post{}, &types.PostInfo{}, nil)
	require.NoError(t, tab.generateInitialPost(context.Background()))
}

func TestBuilder_UpdatePoets(t *testing.T) {
	r := require.New(t)

	tab := newTestBuilder(t, WithPoETClientInitializer(func(string, PoetConfig) (poetClient, error) {
		poet := NewMockpoetClient(gomock.NewController(t))
		poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte("poetid")}, nil)
		return poet, nil
	}))

	r.Nil(tab.Builder.receivePendingPoetClients())

	err := tab.Builder.UpdatePoETServers(context.Background(), []string{"http://poet0", "http://poet1"})
	r.NoError(err)

	clients := tab.Builder.receivePendingPoetClients()
	r.NotNil(clients)
	r.Len(*clients, 2)
	r.Nil(tab.Builder.receivePendingPoetClients())
}

func TestBuilder_UpdatePoetsUnstable(t *testing.T) {
	r := require.New(t)

	tab := newTestBuilder(t, WithPoETClientInitializer(func(string, PoetConfig) (poetClient, error) {
		poet := NewMockpoetClient(gomock.NewController(t))
		poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte("poetid")}, errors.New("ERROR"))
		return poet, nil
	}))

	err := tab.Builder.UpdatePoETServers(context.Background(), []string{"http://poet0", "http://poet1"})
	r.ErrorIs(err, ErrPoetServiceUnstable)
	r.Nil(tab.receivePendingPoetClients())
}

func TestWaitPositioningAtx(t *testing.T) {
	genesis := time.Now()
	for _, tc := range []struct {
		desc         string
		shift, grace time.Duration

		expect string
	}{
		{"no wait", 100 * time.Millisecond, 100 * time.Millisecond, ""},
		{"wait", 100 * time.Millisecond, 0, ""},
		{"round started", 0, 0, "poet round already started"},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			tab := newTestBuilder(t, WithPoetConfig(PoetConfig{
				PhaseShift:  tc.shift,
				GracePeriod: tc.grace,
			}))
			tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
			tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(func(lid types.LayerID) time.Time {
				return genesis.Add(time.Duration(lid) * layerDuration)
			}).AnyTimes()

			// everything else are stubs that are irrelevant for the test
			tab.mpostClient.EXPECT().Info(gomock.Any()).Return(&types.PostInfo{}, nil).AnyTimes()
			tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).Return(&types.NIPost{}, nil).AnyTimes()
			closed := make(chan struct{})
			close(closed)
			tab.mclock.EXPECT().AwaitLayer(types.EpochID(1).FirstLayer()).Return(closed).AnyTimes()
			tab.mclock.EXPECT().AwaitLayer(types.EpochID(2).FirstLayer()).Return(make(chan struct{})).AnyTimes()
			tab.mpub.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

			err := tab.PublishActivationTx(context.Background())
			if len(tc.expect) > 0 {
				require.ErrorContains(t, err, tc.expect)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRegossip(t *testing.T) {
	layer := types.LayerID(10)
	t.Run("not found", func(t *testing.T) {
		h := newTestBuilder(t)
		h.mclock.EXPECT().CurrentLayer().Return(layer)
		require.NoError(t, h.Regossip(context.Background()))
	})
	t.Run("success", func(t *testing.T) {
		h := newTestBuilder(t)
		atx := newActivationTx(t,
			h.signer, 0, types.EmptyATXID, types.EmptyATXID, nil,
			layer.GetEpoch(), 0, 1, types.Address{}, 1, &types.NIPost{})
		require.NoError(t, atxs.Add(h.cdb.Database, atx))
		blob, err := atxs.GetBlob(h.cdb.Database, atx.ID().Bytes())
		require.NoError(t, err)
		h.mclock.EXPECT().CurrentLayer().Return(layer)

		ctx := context.Background()
		h.mpub.EXPECT().Publish(ctx, pubsub.AtxProtocol, blob)
		require.NoError(t, h.Regossip(ctx))
	})
	t.Run("checkpointed", func(t *testing.T) {
		h := newTestBuilder(t)
		require.NoError(t, atxs.AddCheckpointed(h.cdb.Database,
			&atxs.CheckpointAtx{ID: types.ATXID{1}, Epoch: layer.GetEpoch(), SmesherID: h.sig.NodeID()}))
		h.mclock.EXPECT().CurrentLayer().Return(layer)
		require.NoError(t, h.Regossip(context.Background()))
	})
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
