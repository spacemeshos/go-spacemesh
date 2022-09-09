package syncer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	mmocks "github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	layersPerEpoch = 3
	never          = time.Second * 60 * 24
)

type mockLayerTicker struct {
	current atomic.Value
}

func newMockLayerTicker() *mockLayerTicker {
	mt := &mockLayerTicker{}
	mt.current.Store(types.NewLayerID(1))
	return mt
}

func (mlt *mockLayerTicker) advanceToLayer(layerID types.LayerID) {
	mlt.current.Store(layerID)
}

func (mlt *mockLayerTicker) GetCurrentLayer() types.LayerID {
	return mlt.current.Load().(types.LayerID)
}

type testSyncer struct {
	syncer  *Syncer
	cdb     *datastore.CachedDB
	msh     *mesh.Mesh
	mTicker *mockLayerTicker

	mLyrFetcher   *mocks.MocklayerFetcher
	mBeacon       *smocks.MockBeaconGetter
	mLyrPatrol    *mocks.MocklayerPatrol
	mLyrProcessor *mocks.MocklayerProcessor
	mConState     *mmocks.MockconservativeState
	mTortoise     *mmocks.Mocktortoise
}

func newTestSyncer(ctx context.Context, t *testing.T, interval time.Duration, defaultMocked bool) *testSyncer {
	types.SetLayersPerEpoch(layersPerEpoch)
	lg := logtest.New(t)

	mt := newMockLayerTicker()
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)

	ctrl := gomock.NewController(t)
	mcs := mmocks.NewMockconservativeState(ctrl)
	mtrt := mmocks.NewMocktortoise(ctrl)
	mb := smocks.NewMockBeaconGetter(ctrl)
	if defaultMocked {
		mcs.EXPECT().GetStateRoot().AnyTimes()
		mtrt.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, lid types.LayerID) types.LayerID {
				return lid.Sub(1)
			}).AnyTimes()
	}
	mm, err := mesh.NewMesh(cdb, mtrt, mcs, lg)
	require.NoError(t, err)

	mp := mocks.NewMocklayerPatrol(ctrl)
	mf := mocks.NewMocklayerFetcher(ctrl)
	mlp := mocks.NewMocklayerProcessor(ctrl)
	cfg := Configuration{
		SyncInterval:     interval,
		SyncCertDistance: 4,
		HareDelayLayers:  5,
	}
	return &testSyncer{
		syncer:        NewSyncer(ctx, cfg, mt, mb, mm, mf, mp, lg.WithName("test_sync")),
		cdb:           cdb,
		msh:           mm,
		mTicker:       mt,
		mLyrFetcher:   mf,
		mBeacon:       mb,
		mLyrPatrol:    mp,
		mLyrProcessor: mlp,
		mConState:     mcs,
		mTortoise:     mtrt,
	}
}

func okCh() chan fetch.LayerPromiseResult {
	ch := make(chan fetch.LayerPromiseResult, 1)
	close(ch)
	return ch
}

func failedCh() chan fetch.LayerPromiseResult {
	ch := make(chan fetch.LayerPromiseResult, 1)
	ch <- fetch.LayerPromiseResult{
		Err: errors.New("something baaahhhhhhd"),
	}
	close(ch)
	return ch
}

func newSyncerWithoutSyncTimer(t *testing.T) *testSyncer {
	ctx := context.TODO()
	ts := newTestSyncer(ctx, t, never, true)
	ts.syncer.syncTimer.Stop()
	ts.syncer.validateTimer.Stop()
	return ts
}

func TestStartAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	ts := newTestSyncer(ctx, t, time.Millisecond*5, true)

	syncedCh := make(chan struct{})
	ts.syncer.RegisterChForSynced(context.TODO(), syncedCh)

	require.False(t, ts.syncer.IsSynced(context.TODO()))
	require.False(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())

	// the node is synced when current layer is <= 1
	ts.syncer.Start(context.TODO())
	<-syncedCh
	require.True(t, ts.syncer.IsSynced(context.TODO()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())

	cancel()
	require.True(t, ts.syncer.isClosed())
	require.False(t, ts.syncer.synchronize(context.TODO()))
	ts.syncer.Close()
}

func TestSynchronize_OnlyOneSynchronize(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	current := types.NewLayerID(10)
	ts.mTicker.advanceToLayer(current)
	ts.syncer.Start(context.TODO())

	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(failedCh()).AnyTimes()
	gLayer := types.GetEffectiveGenesis()

	ch := make(chan fetch.LayerPromiseResult)
	started := make(chan struct{}, 1)
	for lid := gLayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		if lid == gLayer.Add(1) {
			ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
				func(context.Context, types.LayerID) chan fetch.LayerPromiseResult {
					close(started)
					return ch
				},
			)
		} else {
			ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lid).Return(ch)
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		require.True(t, ts.syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	<-started
	require.False(t, ts.syncer.synchronize(context.TODO()))
	// allow synchronize to finish
	close(ch)
	wg.Wait()

	require.Equal(t, uint64(1), ts.syncer.run)
	ts.syncer.Close()
}

func TestSynchronize_AllGood(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(10)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(failedCh()).AnyTimes()
	for epoch := gLayer.GetEpoch(); epoch <= current.GetEpoch(); epoch++ {
		ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch).Return(nil)
	}
	for lid := gLayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lid).Return(okCh())
	}

	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.Equal(t, current.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current.GetEpoch(), ts.syncer.getLastSyncedATXs())
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))
}

func TestSynchronize_FetchLayerDataFailed(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(failedCh()).AnyTimes()
	lyr := current.Sub(1)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gLayer.GetEpoch()).Return(nil)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch()).Return(nil)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(failedCh())

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.Equal(t, lyr.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current.GetEpoch(), ts.syncer.getLastSyncedATXs())
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))
}

func TestSynchronize_FailedInitialATXsSync(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	failedEpoch := types.EpochID(4)
	current := types.NewLayerID(layersPerEpoch * uint32(failedEpoch+1))
	ts.mTicker.advanceToLayer(current)
	for epoch := types.GetEffectiveGenesis().GetEpoch(); epoch < failedEpoch; epoch++ {
		ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch).Return(nil)
	}
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), failedEpoch).Return(errors.New("no ATXs. should fail sync"))

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	require.Equal(t, failedEpoch-1, ts.syncer.getLastSyncedATXs())
	require.False(t, ts.syncer.dataSynced())
	require.False(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))
}

func startWithSyncedState(t *testing.T, ts *testSyncer) types.LayerID {
	t.Helper()

	gLayer := types.GetEffectiveGenesis()
	ts.mTicker.advanceToLayer(gLayer)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gLayer.GetEpoch()).Return(nil)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(failedCh()).AnyTimes()
	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) chan fetch.LayerPromiseResult {
			require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), got))
			return okCh()
		})

	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.TODO()))
	return current
}

func TestSynchronize_SyncEpochATXAtFirstLayer(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)

	require.Equal(t, lyr.GetEpoch()-1, ts.syncer.getLastSyncedATXs())
	current := lyr.Add(2)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch()-1).Return(nil)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), gomock.Any()).Return(okCh()).Times(2)

	require.True(t, ts.syncer.synchronize(context.TODO()))
}

func TestSynchronize_StaySyncedUponFailure(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(failedCh())

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.TODO()))
}

func TestSynchronize_BecomeNotSyncedUponFailureIfNoGossip(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(failedCh())

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))
}

// test the case where the node originally starts from notSynced and eventually becomes synced.
func TestFromNotSyncedToSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(5)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(failedCh()).AnyTimes()
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(failedCh())

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID) chan fetch.LayerPromiseResult {
				require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), got))
				return okCh()
			})
	}
	require.True(t, ts.syncer.synchronize(context.TODO()))
	// node should be in gossip sync state
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, ts)
}

// test the case where the node originally starts from notSynced, advances to gossipSync, but falls behind
// to notSynced.
func TestFromGossipSyncToNotSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(failedCh()).AnyTimes()
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) chan fetch.LayerPromiseResult {
			require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), got))
			return okCh()
		})

	require.True(t, ts.syncer.synchronize(context.TODO()))
	// node should be in gossip sync state
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	lyr = lyr.Add(1)
	current = current.Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(failedCh())
	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID) chan fetch.LayerPromiseResult {
				require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), got))
				return okCh()
			})
	}
	require.True(t, ts.syncer.synchronize(context.TODO()))
	// the node should enter gossipSync again
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, ts)
}

// test the case where the node was originally synced, and somehow gets out of sync, but
// eventually become synced again.
func TestFromSyncedToNotSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(failedCh()).AnyTimes()
	syncedCh := make(chan struct{})
	ts.syncer.RegisterChForSynced(context.TODO(), syncedCh)

	require.True(t, ts.syncer.synchronize(context.TODO()))
	<-syncedCh
	require.True(t, ts.syncer.IsSynced(context.TODO()))

	// cause the syncer to get out of synced and then wait again
	lyr := types.GetEffectiveGenesis().Add(1)
	current := ts.msh.LatestLayer().Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(failedCh())

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID) chan fetch.LayerPromiseResult {
				require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), got))
				return okCh()
			})
	}
	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, ts)
}

func waitOutGossipSync(t *testing.T, current types.LayerID, ts *testSyncer) {
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	// next layer will be still gossip syncing
	require.Equal(t, types.NewLayerID(2).Uint32(), numGossipSyncLayers)
	require.Equal(t, current.Add(numGossipSyncLayers), ts.syncer.getTargetSyncedLayer())

	lyr := current
	current = current.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) chan fetch.LayerPromiseResult {
			require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), got))
			return okCh()
		})
	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	// done one full layer of gossip sync, now it is synced
	syncedCh := make(chan struct{})
	ts.syncer.RegisterChForSynced(context.TODO(), syncedCh)
	lyr = lyr.Add(1)
	current = current.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) chan fetch.LayerPromiseResult {
			require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), got))
			return okCh()
		})
	require.True(t, ts.syncer.synchronize(context.TODO()))
	<-syncedCh
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.TODO()))
}

func TestSyncMissingLayer(t *testing.T) {
	ts := newTestSyncer(context.TODO(), t, never, false)
	genesis := types.GetEffectiveGenesis()
	failed := genesis.Add(2)
	last := genesis.Add(4)
	ts.mTicker.advanceToLayer(last)

	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(okCh()).AnyTimes()
	ts.mConState.EXPECT().GetStateRoot().AnyTimes()
	ts.mLyrPatrol.EXPECT().IsHareInCharge(gomock.Any()).Return(false).AnyTimes()
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil).AnyTimes()

	block := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{
		LayerIndex: failed,
		TxIDs:      []types.TransactionID{{1, 1, 1}},
	})
	require.NoError(t, blocks.Add(ts.cdb, block))
	require.NoError(t, layers.SetHareOutput(ts.cdb, failed, block.ID()))
	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		if lid != failed {
			require.NoError(t, layers.SetHareOutput(ts.cdb, lid, types.EmptyBlockID))
		}
	}

	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lid).Return(okCh())
		ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
		if lid.Before(failed) {
			require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), lid))
		}
		if lid == failed {
			errMissingTXs := errors.New("missing TXs")
			ts.mConState.EXPECT().ApplyLayer(gomock.Any(), block).DoAndReturn(
				func(_ context.Context, got *types.Block) error {
					require.Equal(t, block.ID(), got.ID())
					return errMissingTXs
				})
		}
	}

	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.Equal(t, last.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, failed, ts.msh.MissingLayer())
	require.Equal(t, last.Sub(1), ts.msh.ProcessedLayer())
	require.Equal(t, failed.Sub(1), ts.msh.LatestLayerInState())

	// test that synchronize will sync from missing layer again
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), failed).Return(okCh())
	require.True(t, ts.syncer.synchronize(context.TODO()))

	for lid := failed; lid.Before(last); lid = lid.Add(1) {
		ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
		if lid == failed {
			ts.mConState.EXPECT().ApplyLayer(gomock.Any(), block).DoAndReturn(
				func(_ context.Context, got *types.Block) error {
					require.Equal(t, block.ID(), got.ID())
					return nil
				})
		} else {
			require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), lid))
		}
	}
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.Equal(t, types.LayerID{}, ts.msh.MissingLayer())
	require.Equal(t, last.Sub(1), ts.msh.ProcessedLayer())
	require.Equal(t, last.Sub(1), ts.msh.LatestLayerInState())
}

func TestSync_AlsoSyncProcessedLayer(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mLyrFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).Return(okCh()).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)

	// simulate hare advancing the mesh forward
	require.NoError(t, ts.msh.ProcessLayerPerHareOutput(context.TODO(), lyr, types.EmptyBlockID))
	require.Equal(t, lyr, ts.msh.ProcessedLayer())

	// no data sync should happen
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	ts.mLyrFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(okCh())
	require.True(t, ts.syncer.synchronize(context.TODO()))
	// but last synced is updated
	require.Equal(t, lyr, ts.syncer.getLastSyncedLayer())
}

func TestProcessLayers_AllGood(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(10)
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	ts.mTicker.advanceToLayer(current)
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), lid))
		ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
		ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
		ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(okCh())
	}
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_DespiteMissingCert(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(10)
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	ts.mTicker.advanceToLayer(current)
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		// not calling SetZeroBlockLayer() on this layer will cause mesh.ProcessLayer() to fail
		ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
		ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
		ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(okCh())
	}
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_BeaconNotAvailable(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, errBeaconNotAvailable)
	require.False(t, ts.syncer.stateSynced())
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errBeaconNotAvailable)
	require.False(t, ts.syncer.stateSynced())
}

func TestProcessLayers_ATXsNotSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(10)
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	ts.mTicker.advanceToLayer(current)
	require.False(t, ts.syncer.stateSynced())
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errATXsNotSynced)
	require.False(t, ts.syncer.stateSynced())
}

func TestProcessLayers_Shutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	ts := newTestSyncer(ctx, t, never, true)
	ts.syncer.setATXSynced()

	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))

	cancel()
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errShuttingDown)
	require.False(t, ts.syncer.stateSynced())
}

func TestProcessLayers_HareIsStillWorking(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))

	require.False(t, ts.syncer.stateSynced())
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
	ts.mLyrPatrol.EXPECT().IsHareInCharge(lastSynced).Return(true)
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errHareInCharge)
	require.False(t, ts.syncer.stateSynced())

	require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), lastSynced))
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
	ts.mLyrPatrol.EXPECT().IsHareInCharge(lastSynced).Return(false)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lastSynced).Return(okCh())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_HareTakesTooLong(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	glayer := types.GetEffectiveGenesis()
	lastSynced := glayer.Add(ts.syncer.conf.HareDelayLayers)
	ts.syncer.setLastSyncedLayer(lastSynced)
	current := lastSynced.Add(1)
	ts.mTicker.advanceToLayer(current)
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		require.NoError(t, ts.msh.SetZeroBlockLayer(context.TODO(), lid))
		ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
		if lid == glayer.Add(1) {
			ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(true)
		} else {
			ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
		}
		ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(okCh())
	}
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_OpinionsOptional(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
	ts.mLyrPatrol.EXPECT().IsHareInCharge(lastSynced).Return(false)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lastSynced).Return(failedCh())
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}
