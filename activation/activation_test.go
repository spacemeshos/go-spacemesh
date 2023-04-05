package activation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
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
	"github.com/spacemeshos/go-spacemesh/sql/kvstore"
)

// ========== Vars / Consts ==========

const (
	layersPerEpoch                 = 10
	layerDuration                  = time.Second
	postGenesisEpoch types.EpochID = 2

	testTickSize = 1
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)
	res := m.Run()
	os.Exit(res)
}

// ========== Helper functions ==========

func newChallenge(sequence uint64, prevAtxID, posAtxID types.ATXID, pubLayerID types.LayerID, cATX *types.ATXID) types.NIPostChallenge {
	return types.NIPostChallenge{
		Sequence:       sequence,
		PrevATXID:      prevAtxID,
		PubLayerID:     pubLayerID,
		PositioningATX: posAtxID,
		CommitmentATX:  cATX,
	}
}

func newAtx(t testing.TB, sig *signing.EdSigner, challenge types.NIPostChallenge, nipost *types.NIPost, numUnits uint32, coinbase types.Address) *types.ActivationTx {
	atx := types.NewActivationTx(challenge, coinbase, nipost, numUnits, nil, nil)
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
	pubLayerID types.LayerID,
	startTick, numTicks uint64,
	coinbase types.Address,
	numUnits uint32,
	nipost *types.NIPost,
) *types.VerifiedActivationTx {
	challenge := newChallenge(sequence, prevATX, positioningATX, pubLayerID, cATX)
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

	mhdlr   *MockatxHandler
	mpub    *mocks.MockPublisher
	mnipost *MocknipostBuilder
	mpost   *MockpostSetupProvider
	mclock  *MocklayerClock
	msync   *Mocksyncer
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
		mhdlr:       NewMockatxHandler(ctrl),
		mpub:        mocks.NewMockPublisher(ctrl),
		mnipost:     NewMocknipostBuilder(ctrl),
		mpost:       NewMockpostSetupProvider(ctrl),
		mclock:      NewMocklayerClock(ctrl),
		msync:       NewMocksyncer(ctrl),
	}

	cfg := Config{
		CoinbaseAccount: tab.coinbase,
		GoldenATXID:     tab.goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(func() chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).AnyTimes()

	b := NewBuilder(cfg, tab.nodeID, tab.sig, tab.cdb, tab.mhdlr, tab.mpub, tab.mnipost, tab.mpost,
		tab.mclock, tab.msync, lg, opts...)
	b.initialPost = &types.Post{
		Nonce:   0,
		Indices: make([]byte, 10),
	}
	b.initialPostMeta = &types.PostMetadata{}
	tab.Builder = b
	return tab
}

func assertLastAtx(r *require.Assertions, nodeID types.NodeID, poetRef types.Hash32, newAtx *types.ActivationTx, posAtx, prevAtx *types.VerifiedActivationTx, layersPerEpoch uint32) {
	atx := newAtx
	r.Equal(nodeID, atx.SmesherID)
	if prevAtx != nil {
		r.Equal(prevAtx.Sequence+1, atx.Sequence)
		r.Equal(prevAtx.ID(), atx.PrevATXID)
		r.Nil(atx.InitialPost)
		r.Nil(atx.InitialPostIndices)
		r.Nil(atx.VRFNonce)
	} else {
		r.Zero(atx.Sequence)
		r.Equal(types.EmptyATXID, atx.PrevATXID)
		r.NotNil(atx.InitialPost)
		r.NotNil(atx.InitialPostIndices)
		r.NotNil(atx.VRFNonce)
	}
	r.Equal(posAtx.ID(), atx.PositioningATX)
	r.Equal(posAtx.PubLayerID.Add(layersPerEpoch), atx.PubLayerID)
	r.Equal(poetRef, atx.GetPoetProofRef())
}

func publishAtx(
	t *testing.T,
	tab *testAtxBuilder,
	posAtxId types.ATXID,
	posAtxLayer types.LayerID,
	currLayer *types.LayerID, // pointer to keep current layer consistent across calls
	buildNIPostLayerDuration uint32,
) (*types.ActivationTx, error) {
	t.Helper()

	publishEpoch := posAtxLayer.GetEpoch() + 1
	tab.mclock.EXPECT().CurrentLayer().Return(*currLayer).AnyTimes()
	tab.mhdlr.EXPECT().GetPosAtxID().Return(posAtxId, nil)
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(*currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, time.Duration, error) {
			*currLayer = currLayer.Add(buildNIPostLayerDuration)
			return newNIPostWithChallenge(challenge.Hash(), []byte("66666")), 0, nil
		})
	ch := make(chan struct{})
	close(ch)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) chan struct{} {
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
	never := make(chan struct{})
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(ch)
	tab.mclock.EXPECT().AwaitLayer((publishEpoch + 2).FirstLayer()).Return(never)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any()).Do(
		func(got types.ATXID) {
			require.Equal(t, built.ID(), got)
		})
	// create and publish ATX
	err := tab.PublishActivationTx(context.Background())
	return built, err
}

func addPrevAtx(t *testing.T, db sql.Executor, epoch types.EpochID, sig *signing.EdSigner) *types.VerifiedActivationTx {
	challenge := types.NIPostChallenge{
		PubLayerID: epoch.FirstLayer(),
	}
	atx := types.NewActivationTx(challenge, types.Address{}, nil, 2, nil, nil)
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

func TestBuilder_waitForFirstATX(t *testing.T) {
	t.Run("genesis", func(t *testing.T) {
		tab := newTestBuilder(t)
		tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0))
		require.False(t, tab.waitForFirstATX(context.Background()))
	})

	// previous ATX not for current epoch -> need to wait
	t.Run("new miner", func(t *testing.T) {
		tab := newTestBuilder(t)
		current := types.LayerID(layersPerEpoch * 2) // first layer of epoch 2
		addPrevAtx(t, tab.cdb, current.GetEpoch()-1, tab.sig)
		tab.mclock.EXPECT().CurrentLayer().Return(current).AnyTimes()
		tab.mclock.EXPECT().LayerToTime(current).Return(time.Now().Add(100 * time.Millisecond)).AnyTimes()
		require.True(t, tab.waitForFirstATX(context.Background()))
	})

	t.Run("new miner too late", func(t *testing.T) {
		poetCfg := PoetConfig{
			PhaseShift:  5 * time.Millisecond,
			CycleGap:    2 * time.Millisecond,
			GracePeriod: time.Millisecond,
		}
		tab := newTestBuilder(t, WithPoetConfig(poetCfg))
		current := types.LayerID(layersPerEpoch * 2) // first layer of epoch 2
		next := current.Add(layersPerEpoch)
		addPrevAtx(t, tab.cdb, current.GetEpoch()-1, tab.sig)
		tab.mclock.EXPECT().CurrentLayer().Return(current)
		tab.mclock.EXPECT().LayerToTime(current).Return(time.Now().Add(-5 * time.Millisecond))
		tab.mclock.EXPECT().LayerToTime(next).Return(time.Now().Add(100 * time.Millisecond))
		tab.mclock.EXPECT().CurrentLayer().Return(current.Add(layersPerEpoch)).AnyTimes()
		require.True(t, tab.waitForFirstATX(context.Background()))
	})

	// previous ATX for current epoch -> no wait
	t.Run("existing miner", func(t *testing.T) {
		tab := newTestBuilder(t)
		current := types.LayerID(layersPerEpoch * 2) // first layer of epoch 2
		tab.mclock.EXPECT().CurrentLayer().Return(current)
		addPrevAtx(t, tab.cdb, current.GetEpoch(), tab.sig)
		require.False(t, tab.waitForFirstATX(context.Background()))
	})
}

func TestBuilder_StartSmeshingCoinbase(t *testing.T) {
	tab := newTestBuilder(t)
	coinbase := types.Address{1, 1, 1}
	postSetupOpts := PostSetupOpts{}

	tab.mpost.EXPECT().StartSession(gomock.Any(), postSetupOpts).AnyTimes()
	tab.mpost.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()
	require.NoError(t, tab.StartSmeshing(coinbase, postSetupOpts))
	require.Equal(t, coinbase, tab.Coinbase())

	// calling StartSmeshing more than once before calling StopSmeshing is an error
	require.ErrorContains(t, tab.StartSmeshing(coinbase, postSetupOpts), "already started")

	tab.mpost.EXPECT().Reset()
	require.NoError(t, tab.StopSmeshing(true))
}

func TestBuilder_RestartSmeshing(t *testing.T) {
	getBuilder := func(t *testing.T) *Builder {
		tab := newTestBuilder(t)
		tab.mpost.EXPECT().StartSession(gomock.Any(), gomock.Any()).AnyTimes()
		tab.mpost.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).AnyTimes()
		tab.mpost.EXPECT().Reset().AnyTimes()
		ch := make(chan struct{})
		close(ch)
		tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(ch).AnyTimes()
		tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
		tab.mhdlr.EXPECT().GetPosAtxID().Return(types.ATXID{1, 2, 3}, nil).AnyTimes()
		return tab.Builder
	}

	t.Run("Single threaded", func(t *testing.T) {
		builder := getBuilder(t)
		for i := 0; i < 100; i++ {
			require.NoError(t, builder.StartSmeshing(types.Address{}, PostSetupOpts{}))
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
					_ = builder.StartSmeshing(types.Address{}, PostSetupOpts{})
					_ = builder.StopSmeshing(true)
				}
				return nil
			})
		}
		require.NoError(t, eg.Wait())
	})
}

func TestBuilder_StopSmeshing_failsWhenNotStarted(t *testing.T) {
	tab := newTestBuilder(t)
	require.ErrorContains(t, tab.StopSmeshing(true), "not started")
}

func TestBuilder_StopSmeshing_OnPoSTError(t *testing.T) {
	tab := newTestBuilder(t)
	tab.mpost.EXPECT().StartSession(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	tab.mpost.EXPECT().GenerateProof(gomock.Any(), gomock.Any()).Return(nil, nil, nil).AnyTimes()
	ch := make(chan struct{})
	close(ch)
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(ch).AnyTimes()
	tab.mclock.EXPECT().CurrentLayer().Return(types.LayerID(0)).AnyTimes()
	tab.mhdlr.EXPECT().GetPosAtxID().Return(types.ATXID{1, 2, 3}, nil).AnyTimes()
	tab.msync.EXPECT().RegisterForATXSynced().Return(ch).AnyTimes()
	require.NoError(t, tab.StartSmeshing(tab.coinbase, PostSetupOpts{}))

	tab.mpost.EXPECT().Reset().Return(errors.New("couldn't delete files"))
	require.Error(t, tab.StopSmeshing(true))
	require.False(t, tab.Smeshing())
}

func TestBuilder_PublishActivationTx_HappyFlow(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := postGenesisEpoch.FirstLayer()
	currLayer := posAtxLayer
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	// create and publish ATX
	atx1, err := publishAtx(t, tab, prevAtx.ID(), posAtxLayer, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx1)

	// create and publish another ATX
	currLayer = (posAtxLayer.GetEpoch() + 1).FirstLayer()
	atx2, err := publishAtx(t, tab, atx1.ID(), atx1.PubLayerID, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotEqual(t, atx1, atx2)
	require.Equal(t, atx1.TargetEpoch()+1, atx2.TargetEpoch())
}

// TestBuilder_PublishActivationTx_StaleChallenge checks if
// Builder::PublishActivationTx properly detects that a challenge it constructed
// is stale and the poet round has already started.
func TestBuilder_PublishActivationTx_StaleChallenge(t *testing.T) {
	// Arrange
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := postGenesisEpoch.FirstLayer()
	// current layer is too late to be able to build a nipost on time
	currLayer := (postGenesisEpoch + 1).FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	tab.mhdlr.EXPECT().GetPosAtxID().Return(prevAtx.ID(), nil)
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()

	// Act & Verify
	err = tab.PublishActivationTx(context.Background())
	require.ErrorIs(t, err, ErrATXChallengeExpired)
	require.ErrorContains(t, err, "poet round has already started")
}

// TestBuilder_Loop_WaitsOnStaleChallenge checks if loop waits between attempts
// failing with ErrATXChallengeExpired.
func TestBuilder_Loop_WaitsOnStaleChallenge(t *testing.T) {
	// Arrange
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := postGenesisEpoch.FirstLayer()
	// current layer is too late to be able to build a nipost on time
	currLayer := (postGenesisEpoch + 1).FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	tab.mhdlr.EXPECT().GetPosAtxID().Return(prevAtx.ID(), nil)
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tab.mclock.EXPECT().AwaitLayer(currLayer.Add(1)).Do(func(got types.LayerID) { cancel() })

	// Act & Verify
	var eg errgroup.Group
	eg.Go(func() error {
		tab.loop(ctx)
		return nil
	})

	eg.Wait()
}

func TestBuilder_PublishActivationTx_FaultyNet(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := postGenesisEpoch.FirstLayer()
	currLayer := posAtxLayer
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	publishEpoch := posAtxLayer.GetEpoch() + 1
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mhdlr.EXPECT().GetPosAtxID().Return(prevAtx.ID(), nil)
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, time.Duration, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(challenge.Hash(), []byte("66666")), 0, nil
		})
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) chan struct{} {
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
	never := make(chan struct{})
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(never)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any())
	// create and publish ATX
	err = tab.PublishActivationTx(context.Background())
	require.ErrorIs(t, err, publishErr)
	require.NotNil(t, built)

	// now causing it to publish again, it should use the same atx
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(never)
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
	expireEpoch := publishEpoch + 2
	tab.mclock.EXPECT().AwaitLayer(expireEpoch.FirstLayer()).Return(done)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any()).Do(
		func(got types.ATXID) {
			require.Equal(t, built.ID(), got)
		})
	require.ErrorIs(t, tab.PublishActivationTx(context.Background()), ErrATXChallengeExpired)

	// if the network works and we try to publish a new ATX, the timeout should result in a clean state (so a NIPost should be built)
	posAtxLayer = posAtxLayer.Add(layersPerEpoch + 1)
	challenge = newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))
	built2, err := publishAtx(t, tab, posAtx.ID(), posAtxLayer, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPost, built2.NIPost)
	require.Equal(t, built.TargetEpoch()+1, built2.TargetEpoch())
}

func TestBuilder_PublishActivationTx_RebuildNIPostWhenTargetEpochPassed(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := types.EpochID(2).FirstLayer()
	currLayer := posAtxLayer
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	publishEpoch := posAtxLayer.GetEpoch() + 1
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(
		func() types.LayerID {
			return currLayer
		}).AnyTimes()
	tab.mhdlr.EXPECT().GetPosAtxID().Return(prevAtx.ID(), nil)
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, time.Duration, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(challenge.Hash(), []byte("66666")), 0, nil
		})
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) chan struct{} {
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
	never := make(chan struct{})
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(never)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any())
	// create and publish ATX
	err = tab.PublishActivationTx(context.Background())
	require.ErrorIs(t, err, publishErr)
	require.NotNil(t, built)

	// We started building the NIPost in epoch 2, the publication epoch should have been 3. We should abort the ATX and
	// start over if the target epoch (4) has passed, so we'll start the ATX builder in epoch 5 and ensure it discards
	// a stale challenge and builds a new NIPost.
	posAtxLayer = types.EpochID(5).FirstLayer()
	currLayer = posAtxLayer
	challenge = newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))
	built2, err := publishAtx(t, tab, posAtx.ID(), posAtxLayer, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPost, built2.NIPost)
	require.Equal(t, built.TargetEpoch()+3, built2.TargetEpoch())
}

func TestBuilder_PublishActivationTx_NoPrevATX(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := postGenesisEpoch.FirstLayer()
	currLayer := posAtxLayer
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	otherSigner, err := signing.NewEdSigner()
	require.NoError(t, err)
	posAtx := newAtx(t, otherSigner, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(otherSigner, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))

	// create and publish ATX
	vrfNonce := types.VRFPostIndex(123)
	tab.mpost.EXPECT().VRFNonce().Return(&vrfNonce, nil)
	tab.mpost.EXPECT().CommitmentAtx().Return(types.RandomATXID(), nil)
	atx, err := publishAtx(t, tab, posAtx.ID(), posAtxLayer, &currLayer, layersPerEpoch)
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
	prevAtxPubLayer := postGenesisEpoch.FirstLayer()
	posAtxPubLayer := postGenesisEpoch.FirstLayer()

	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxPubLayer, nil)
	poetBytes := []byte("66666")
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), poetBytes)
	posAtx := newAtx(t, otherSigner, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(otherSigner, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxs.Add(tab.cdb, vPosAtx))

	challenge = newChallenge(0, types.EmptyATXID, posAtx.ID(), prevAtxPubLayer, nil)
	challenge.InitialPostIndices = initialPost.Indices
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	prevAtx.InitialPost = initialPost
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxs.Add(tab.cdb, vPrevAtx))

	// Act
	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(func() chan struct{} {
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
	tab.mclock.EXPECT().AwaitLayer(vPosAtx.PublishEpoch().FirstLayer().Add(layersPerEpoch)).DoAndReturn(func(layer types.LayerID) chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).Times(1)

	tab.mclock.EXPECT().AwaitLayer(gomock.Not(vPosAtx.PublishEpoch().FirstLayer().Add(layersPerEpoch))).DoAndReturn(func(types.LayerID) chan struct{} {
		ch := make(chan struct{})
		return ch
	}).Times(1)

	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()

	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, int, error) {
			currentLayer = currentLayer.Add(5)
			return newNIPostWithChallenge(challenge.Hash(), poetBytes), 0, nil
		})

	atxChan := make(chan struct{})
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(atxChan)
	tab.mhdlr.EXPECT().GetPosAtxID().Return(vPosAtx.ID(), nil)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any())

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
		r.Nil(atx.InitialPostIndices)

		r.Equal(posAtx.ID(), atx.PositioningATX)
		r.Equal(posAtxPubLayer.Add(layersPerEpoch), atx.PubLayerID)
		r.Equal(types.BytesToHash(poetBytes), atx.GetPoetProofRef())

		close(atxChan)
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
	posAtxLayer := postGenesisEpoch.FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	poetBytes := []byte("66666")
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), poetBytes)
	posAtx := newAtx(t, otherSigner, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(otherSigner, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	r.NoError(err)
	r.NoError(atxs.Add(tab.cdb, vPosAtx))

	// Act & Assert
	tab.msync.EXPECT().RegisterForATXSynced().DoAndReturn(func() chan struct{} {
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
	tab.mclock.EXPECT().AwaitLayer(vPosAtx.PublishEpoch().FirstLayer().Add(layersPerEpoch)).DoAndReturn(func(types.LayerID) chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	}).Times(1)

	tab.mclock.EXPECT().AwaitLayer(gomock.Not(vPosAtx.PublishEpoch().FirstLayer().Add(layersPerEpoch))).DoAndReturn(func(types.LayerID) chan struct{} {
		ch := make(chan struct{})
		return ch
	}).Times(1)

	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()

	tab.mpost.EXPECT().CommitmentAtx().Return(types.RandomATXID(), nil).Times(1)
	tab.mpost.EXPECT().VRFNonce().Times(1)

	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, int, error) {
			currentLayer = currentLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(challenge.Hash(), poetBytes), 0, nil
		})

	atxChan := make(chan struct{})
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(atxChan)
	tab.mhdlr.EXPECT().GetPosAtxID().Return(vPosAtx.ID(), nil)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any())

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
		r.NotNil(atx.InitialPostIndices)

		r.Equal(posAtx.ID(), atx.PositioningATX)
		r.Equal(posAtxLayer.Add(layersPerEpoch), atx.PubLayerID)
		r.Equal(types.BytesToHash(poetBytes), atx.GetPoetProofRef())

		close(atxChan)
		return nil
	})

	r.NoError(tab.PublishActivationTx(context.Background()))
}

func TestBuilder_PublishActivationTx_FailsWhenNIPostBuilderFails(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := postGenesisEpoch.FirstLayer()
	currLayer := posAtxLayer
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))

	tab.mclock.EXPECT().CurrentLayer().Return(posAtxLayer).AnyTimes()
	tab.mhdlr.EXPECT().GetPosAtxID().Return(vPosAtx.ID(), nil)
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()
	nipostErr := fmt.Errorf("NIPost builder error")
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).Return(nil, time.Duration(0), nipostErr)
	require.ErrorIs(t, tab.PublishActivationTx(context.Background()), nipostErr)
}

func TestBuilder_PublishActivationTx_Serialize(t *testing.T) {
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	coinbase := types.Address{4, 5, 6}
	atx := newActivationTx(t, sig, 1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, nil, types.LayerID(5), 1, 100, coinbase, 100, nipost)
	require.NoError(t, atxs.Add(cdb, atx))

	act := newActivationTx(t, sig, 2, atx.ID(), atx.ID(), nil, atx.PubLayerID.Add(10), 0, 100, coinbase, 100, nipost)

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
	challenge := newChallenge(1, prevAtx, prevAtx, types.LayerID(15), nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	atx := newAtx(t, tab.sig, challenge, nipost, 100, types.Address{})
	atx.SetMetadata()
	atxBytes, err := codec.Encode(&atx.ATXMetadata)
	require.NoError(t, err)
	err = SignAndFinalizeAtx(tab.signer, atx)
	require.NoError(t, err)

	verifier, err := signing.NewEdVerifier()
	require.NoError(t, err)

	ok := verifier.Verify(signing.ATX, tab.nodeID, atxBytes, atx.Signature)
	require.True(t, ok)
	require.Equal(t, tab.nodeID, atx.SmesherID)
}

func TestBuilder_NIPostPublishRecovery(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	posAtxLayer := postGenesisEpoch.FirstLayer()
	currLayer := posAtxLayer
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), []byte("66666"))
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	publishEpoch := posAtxLayer.GetEpoch() + 1
	tab.mclock.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID { return currLayer }).AnyTimes()
	tab.mhdlr.EXPECT().GetPosAtxID().Return(prevAtx.ID(), nil)
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, time.Duration, error) {
			currLayer = currLayer.Add(layersPerEpoch)
			return newNIPostWithChallenge(challenge.Hash(), []byte("66666")), 0, nil
		})
	done := make(chan struct{})
	close(done)
	tab.mclock.EXPECT().AwaitLayer(publishEpoch.FirstLayer()).DoAndReturn(
		func(got types.LayerID) chan struct{} {
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
	never := make(chan struct{})
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(never)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any())
	// create and publish ATX
	err = tab.PublishActivationTx(context.Background())
	require.ErrorIs(t, err, publishErr)
	require.NotNil(t, built)

	// the challenge remains
	got, err := kvstore.GetNIPostChallenge(tab.cdb)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	// now causing it to publish again, it should use the same atx
	tab.mhdlr.EXPECT().AwaitAtx(gomock.Any()).Return(never)
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
	expireEpoch := publishEpoch + 2
	tab.mclock.EXPECT().AwaitLayer(expireEpoch.FirstLayer()).Return(done)
	tab.mhdlr.EXPECT().UnsubscribeAtx(gomock.Any()).Do(
		func(got types.ATXID) {
			require.Equal(t, built.ID(), got)
		})
	// This 👇 ensures that handing of the challenge succeeded and the code moved on to the next part
	require.ErrorIs(t, tab.PublishActivationTx(context.Background()), ErrATXChallengeExpired)
	got, err = kvstore.GetNIPostChallenge(tab.cdb)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Empty(t, got)

	posAtxLayer = posAtxLayer.Add(layersPerEpoch + 1)
	challenge = newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	posAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, posAtx)
	vPosAtx, err := posAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPosAtx))
	built2, err := publishAtx(t, tab, posAtx.ID(), posAtxLayer, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, built2)
	require.NotEqual(t, built.NIPost, built2.NIPost)
	require.Equal(t, built.TargetEpoch()+1, built2.TargetEpoch())

	got, err = kvstore.GetNIPostChallenge(tab.cdb)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Empty(t, got)
}

func TestBuilder_RetryPublishActivationTx(t *testing.T) {
	retryInterval := 10 * time.Microsecond
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}), WithPoetRetryInterval(retryInterval))
	posAtxLayer := (postGenesisEpoch + 1).FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	poetBytes := []byte("66666")
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), poetBytes)
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	currLayer := posAtxLayer.Add(1)
	tab.mclock.EXPECT().CurrentLayer().Return(currLayer).AnyTimes()
	tab.mhdlr.EXPECT().GetPosAtxID().Return(prevAtx.ID(), nil).AnyTimes()
	tab.mclock.EXPECT().LayerToTime(gomock.Any()).DoAndReturn(
		func(got types.LayerID) time.Time {
			// time.Now() ~= currentLayer
			genesis := time.Now().Add(-time.Duration(currLayer) * layerDuration)
			return genesis.Add(layerDuration * time.Duration(got))
		}).AnyTimes()
	lastOpts := DefaultPostSetupOpts()
	tab.mpost.EXPECT().LastOpts().Return(&lastOpts).AnyTimes()
	tab.mclock.EXPECT().AwaitLayer(gomock.Any()).Return(make(chan struct{})).AnyTimes()

	expectedTries := 3
	tries := 0
	builderConfirmation := make(chan struct{})
	// TODO(dshulyak) maybe measure time difference between attempts. It should be no less than retryInterval
	tab.mnipost.EXPECT().BuildNIPost(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, challenge *types.NIPostChallenge) (*types.NIPost, time.Duration, error) {
			tries++
			if tries == expectedTries {
				close(builderConfirmation)
			} else if tries < expectedTries {
				return nil, 0, ErrPoetServiceUnstable
			}
			return newNIPostWithChallenge(challenge.Hash(), poetBytes), 0, nil
		}).Times(expectedTries)

	ctx, cancel := context.WithCancel(context.Background())
	runnerExit := make(chan struct{})
	go func() {
		tab.loop(ctx)
		close(runnerExit)
	}()
	t.Cleanup(func() {
		cancel()
		<-runnerExit
	})

	select {
	case <-builderConfirmation:
	case <-time.After(time.Second):
		require.FailNow(t, "failed waiting for required number of tries to occur")
	}
}

func TestBuilder_InitialProofGeneratedOnce(t *testing.T) {
	tab := newTestBuilder(t, WithPoetConfig(PoetConfig{PhaseShift: layerDuration * 4}))
	tab.mpost.EXPECT().GenerateProof(gomock.Any(), gomock.Any())
	require.NoError(t, tab.generateProof(context.Background()))

	posAtxLayer := (postGenesisEpoch + 1).FirstLayer()
	challenge := newChallenge(1, types.ATXID{1, 2, 3}, types.ATXID{1, 2, 3}, posAtxLayer, nil)
	poetByte := []byte("66666")
	nipost := newNIPostWithChallenge(types.HexToHash32("55555"), poetByte)
	prevAtx := newAtx(t, tab.sig, challenge, nipost, 2, types.Address{})
	SignAndFinalizeAtx(tab.sig, prevAtx)
	vPrevAtx, err := prevAtx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(tab.cdb, vPrevAtx))

	currLayer := posAtxLayer.Add(1)
	atx, err := publishAtx(t, tab, prevAtx.ID(), posAtxLayer, &currLayer, layersPerEpoch)
	require.NoError(t, err)
	require.NotNil(t, atx)
	assertLastAtx(require.New(t), tab.nodeID, types.BytesToHash(poetByte), atx, vPrevAtx, vPrevAtx, layersPerEpoch)

	// GenerateProof() should not be called again
	require.NoError(t, tab.generateProof(context.Background()))
}

func TestBuilder_UpdatePoets(t *testing.T) {
	r := require.New(t)

	tab := newTestBuilder(t, WithPoETClientInitializer(func(string, PoetConfig) (PoetProvingServiceClient, error) {
		poet := NewMockPoetProvingServiceClient(gomock.NewController(t))
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

	tab := newTestBuilder(t, WithPoETClientInitializer(func(string, PoetConfig) (PoetProvingServiceClient, error) {
		poet := NewMockPoetProvingServiceClient(gomock.NewController(t))
		poet.EXPECT().PoetServiceID(gomock.Any()).AnyTimes().Return(types.PoetServiceID{ServiceID: []byte("poetid")}, errors.New("ERROR"))
		return poet, nil
	}))

	err := tab.Builder.UpdatePoETServers(context.Background(), []string{"http://poet0", "http://poet1"})
	r.ErrorIs(err, ErrPoetServiceUnstable)
	r.Nil(tab.receivePendingPoetClients())
}
