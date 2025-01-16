package syncer

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/mesh"
	mmocks "github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	layersPerEpoch = 3
	never          = time.Second * 60 * 24

	outOfSyncThreshold = 3
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

type mockLayerTicker struct {
	current       atomic.Value
	genesis       time.Time
	layerDuration time.Duration
}

func newMockLayerTicker() *mockLayerTicker {
	mt := &mockLayerTicker{layerDuration: time.Minute}
	mt.current.Store(types.LayerID(1))
	return mt
}

func (mlt *mockLayerTicker) advanceToLayer(layerID types.LayerID) {
	mlt.current.Store(layerID)
}

func (mlt *mockLayerTicker) CurrentLayer() types.LayerID {
	return mlt.current.Load().(types.LayerID)
}

func (mlt *mockLayerTicker) LayerToTime(layerID types.LayerID) time.Time {
	return mlt.genesis.Add(time.Duration(layerID) * mlt.layerDuration)
}

type testSyncer struct {
	tb      testing.TB
	syncer  *Syncer
	cdb     *datastore.CachedDB
	msh     *mesh.Mesh
	mTicker *mockLayerTicker

	mDataFetcher *mocks.MockfetchLogic
	mAtxSyncer   *mocks.MockatxSyncer
	mMalSyncer   *mocks.MockmalSyncer
	mLyrPatrol   *mocks.MocklayerPatrol
	mVm          *mmocks.MockvmState
	mConState    *mmocks.MockconservativeState
	mTortoise    *smocks.MockTortoise
	mCertHdr     *mocks.MockcertHandler
	mForkFinder  *mocks.MockforkFinder
	mASV2        *mocks.MockmultiEpochAtxSyncerV2
}

func (ts *testSyncer) expectMalEnsureInSync(current types.LayerID) {
	ts.mMalSyncer.EXPECT().EnsureInSync(
		gomock.Any(),
		ts.mTicker.LayerToTime(current.GetEpoch().FirstLayer()),
		ts.mTicker.LayerToTime(current.GetEpoch().Add(1).FirstLayer()),
	)
}

func (ts *testSyncer) expectMalDownloadLoop() chan struct{} {
	ch := make(chan struct{})
	ts.mMalSyncer.EXPECT().DownloadLoop(gomock.Any()).
		DoAndReturn(func(context.Context) error {
			close(ch)
			return nil
		})
	ts.tb.Cleanup(func() {
		select {
		case <-ch:
		case <-time.After(10 * time.Second):
			require.FailNow(ts.tb, "timed out waiting for malsync loop start")
		}
	})
	return ch
}

func newTestSyncerWithConfig(tb testing.TB, cfg Config) *testSyncer {
	lg := zaptest.NewLogger(tb)
	mt := newMockLayerTicker()
	ctrl := gomock.NewController(tb)

	ts := &testSyncer{
		tb:           tb,
		mTicker:      mt,
		mDataFetcher: mocks.NewMockfetchLogic(ctrl),
		mAtxSyncer:   mocks.NewMockatxSyncer(ctrl),
		mMalSyncer:   mocks.NewMockmalSyncer(ctrl),
		mLyrPatrol:   mocks.NewMocklayerPatrol(ctrl),
		mVm:          mmocks.NewMockvmState(ctrl),
		mConState:    mmocks.NewMockconservativeState(ctrl),
		mTortoise:    smocks.NewMockTortoise(ctrl),
		mCertHdr:     mocks.NewMockcertHandler(ctrl),
		mForkFinder:  mocks.NewMockforkFinder(ctrl),
		mASV2:        mocks.NewMockmultiEpochAtxSyncerV2(ctrl),
	}
	db := statesql.InMemoryTest(tb)
	ts.cdb = datastore.NewCachedDB(db, lg)
	ts.tb.Cleanup(func() { assert.NoError(tb, ts.cdb.Close()) })
	var err error
	atxsdata := atxsdata.New()
	exec := mesh.NewExecutor(ts.cdb, atxsdata, ts.mVm, ts.mConState, lg)
	ts.msh, err = mesh.NewMesh(db, atxsdata, ts.mTortoise, exec, ts.mConState, lg)
	require.NoError(tb, err)

	ts.syncer, err = NewSyncer(
		ts.cdb,
		ts.mTicker,
		ts.msh,
		ts.mTortoise,
		nil,
		nil,
		nil,
		ts.mLyrPatrol,
		ts.mCertHdr,
		ts.mAtxSyncer,
		ts.mMalSyncer,
		WithConfig(cfg),
		WithLogger(lg),
		withDataFetcher(ts.mDataFetcher),
		withForkFinder(ts.mForkFinder),
		withAtxSyncerV2(ts.mASV2),
	)
	require.NoError(tb, err)
	return ts
}

func defaultTestConfig(interval time.Duration) Config {
	return Config{
		Interval:                 interval,
		GossipDuration:           5 * time.Millisecond,
		EpochEndFraction:         0.66,
		SyncCertDistance:         4,
		HareDelayLayers:          5,
		OutOfSyncThresholdLayers: outOfSyncThreshold,
	}
}

func newTestSyncer(tb testing.TB, interval time.Duration) *testSyncer {
	return newTestSyncerWithConfig(tb, defaultTestConfig(interval))
}

func newSyncerWithoutPeriodicRuns(tb testing.TB) *testSyncer {
	ts := newTestSyncer(tb, never)
	ts.mDataFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return([]p2p.Peer{"non-empty"}).AnyTimes()
	return ts
}

func newSyncerWithoutPeriodicRunsWithConfig(tb testing.TB, cfg Config) *testSyncer {
	cfg.Interval = never
	ts := newTestSyncerWithConfig(tb, cfg)
	ts.mDataFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return([]p2p.Peer{"non-empty"}).AnyTimes()
	return ts
}

func newTestSyncerForState(tb testing.TB) *testSyncer {
	ts := newTestSyncer(tb, never)
	return ts
}

func TestStartAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts := newTestSyncer(t, time.Millisecond*5)

	require.False(t, ts.syncer.IsSynced(ctx))
	require.False(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())

	// the node is synced when current layer is <= 1
	ts.syncer.Start()

	ts.mForkFinder.EXPECT().Purge(false).AnyTimes()
	ts.mDataFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(nil).AnyTimes()
	require.Eventually(t, func() bool {
		return ts.syncer.ListenToATXGossip() && ts.syncer.ListenToGossip() &&
			ts.syncer.IsSynced(ctx)
	}, time.Second, 10*time.Millisecond)

	ts.mASV2.EXPECT().Stop()
	cancel()
	require.False(t, ts.syncer.synchronize(ctx))
	ts.syncer.Close()
}

func TestShutdownWithoutStart(t *testing.T) {
	ts := newTestSyncer(t, time.Millisecond*5)
	ts.syncer.Close()
}

func TestSynchronize_OnlyOneSynchronize(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	current := types.LayerID(10)
	ts.mTicker.advanceToLayer(current)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dlCh := ts.expectMalDownloadLoop()
	ts.syncer.Start()

	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	ts.expectMalEnsureInSync(current)
	gLayer := types.GetEffectiveGenesis()

	started := make(chan struct{}, 1)
	done := make(chan struct{}, 1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), gLayer.Add(1)).DoAndReturn(
		func(context.Context, types.LayerID, ...p2p.Peer) error {
			close(started)
			<-done
			return nil
		},
	)
	for lid := gLayer.Add(2); lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
	}
	var eg errgroup.Group
	eg.Go(func() error {
		if !ts.syncer.synchronize(ctx) {
			return errors.New("synchronize failed")
		}
		return nil
	})
	<-started
	require.False(t, ts.syncer.synchronize(ctx))
	// allow synchronize to finish
	close(done)
	require.NoError(t, eg.Wait())
	<-dlCh

	ts.mASV2.EXPECT().Stop()
	cancel()
	ts.syncer.Close()
}

func advanceState(tb testing.TB, ts *testSyncer, from, to types.LayerID) {
	tb.Helper()
	for lid := from; lid <= to; lid++ {
		require.NoError(
			tb,
			certificates.Add(ts.cdb, lid, &types.Certificate{BlockID: types.EmptyBlockID}),
		)
		ts.mLyrPatrol.EXPECT().IsHareInCharge(lid)
		if lid.Add(ts.syncer.cfg.SyncCertDistance) > ts.mTicker.CurrentLayer() {
			ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid, false, gomock.Any())
		}
		ts.mTortoise.EXPECT().TallyVotes(lid)
		ts.mTortoise.EXPECT().OnApplied(lid, gomock.Any())
		ts.mTortoise.EXPECT().Updates().Return(fixture.RLayers(fixture.RLayer(lid)))
		ts.mVm.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any())
		ts.mConState.EXPECT().UpdateCache(gomock.Any(), lid, gomock.Any(), nil, nil)
		ts.mVm.EXPECT().GetStateRoot()
	}
	require.NoError(tb, ts.syncer.processLayers(context.Background()))
	require.True(tb, ts.syncer.stateSynced())
}

func TestSynchronize_AllGood(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	gLayer := types.GetEffectiveGenesis()
	current1 := gLayer.Add(10)
	ts.mTicker.advanceToLayer(current1)
	// we do non-background download for layers that already passed
	for epoch := gLayer.GetEpoch(); epoch < current1.GetEpoch(); epoch++ {
		downloadUntil := ts.mTicker.LayerToTime((epoch + 1).FirstLayer())
		ts.mAtxSyncer.EXPECT().Download(gomock.Any(), epoch, downloadUntil)
	}
	// we run it in background too
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), current1.GetEpoch()-1, gomock.Any())
	current2 := current1 + types.LayerID(types.GetLayersPerEpoch())

	for currentEpoch := current1.GetEpoch(); currentEpoch < current2.GetEpoch(); currentEpoch++ {
		// see arithmetic in fetchATXsForEpoch for why we expect +2
		// later in test we will advance current layers, such that syncer will have to terminate
		// previously spawned background worker and spawn a new one
		downloadUntil := ts.mTicker.LayerToTime((currentEpoch + 2).FirstLayer())
		ts.mAtxSyncer.EXPECT().
			Download(gomock.Any(), currentEpoch, downloadUntil).
			DoAndReturn(func(ctx context.Context, _ types.EpochID, _ time.Time) error {
				<-ctx.Done()
				return nil
			})
	}

	ts.expectMalEnsureInSync(current1)
	for lid := gLayer.Add(1); lid.Before(current2); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
	}

	var eg errgroup.Group
	eg.Go(func() error {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			return errors.New("node should not be atx synced")
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})
	require.NoError(t, eg.Wait())

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, current1.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current1.Sub(1).GetEpoch(), ts.syncer.lastAtxEpoch())
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	eg.Go(func() error {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			return nil
		case <-time.After(1 * time.Second):
			return errors.New("not atx synced")
		}
	})
	require.NoError(t, eg.Wait())

	ts.mTicker.advanceToLayer(current2)
	require.True(t, ts.syncer.synchronize(context.Background()))

	advanceState(t, ts, gLayer+1, current2-1)
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))
	waitOutGossipSync(t, ts)
}

func TestSynchronize_FetchLayerDataFailed(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	// times 2 as we will also spinup background worker
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gLayer.GetEpoch(), gomock.Any()).Times(2)
	ts.expectMalEnsureInSync(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("meh"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	ts.syncer.waitBackgroundSync()
	require.Equal(t, lyr.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current.GetEpoch()-1, ts.syncer.lastAtxEpoch())
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))
}

func TestSynchronize_FetchMalfeasanceFailed(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	ts.mMalSyncer.EXPECT().EnsureInSync(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("meh"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.EqualValues(t, current.GetEpoch()-1, ts.syncer.lastAtxEpoch())
	require.Equal(t, lyr.Sub(1), ts.syncer.getLastSyncedLayer())
}

func TestSynchronize_FailedInitialATXsSync(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	failedEpoch := types.EpochID(4)
	current := types.LayerID(layersPerEpoch * uint32(failedEpoch+1))
	ts.mTicker.advanceToLayer(current)
	for epoch := types.GetEffectiveGenesis().GetEpoch(); epoch < failedEpoch; epoch++ {
		ts.mAtxSyncer.EXPECT().Download(gomock.Any(), epoch, gomock.Any())
	}
	ts.mAtxSyncer.EXPECT().
		Download(gomock.Any(), failedEpoch, gomock.Any()).
		Return(errors.New("no ATXs. should fail sync"))

	var eg errgroup.Group
	eg.Go(func() error {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			return errors.New("node should not be atx synced")
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})
	require.NoError(t, eg.Wait())

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	require.Equal(t, failedEpoch-1, ts.syncer.lastAtxEpoch())
	require.False(t, ts.syncer.dataSynced())
	require.False(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	eg.Go(func() error {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			return errors.New("node should not be atx synced")
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})
	require.NoError(t, eg.Wait())
}

func startWithSyncedState(tb testing.TB, ts *testSyncer) types.LayerID {
	tb.Helper()

	gLayer := types.GetEffectiveGenesis()
	ts.mTicker.advanceToLayer(gLayer)
	ts.expectMalEnsureInSync(gLayer)
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gLayer.GetEpoch(), gomock.Any())
	require.True(tb, ts.syncer.synchronize(context.Background()))
	ts.syncer.waitBackgroundSync()
	require.True(tb, ts.syncer.ListenToATXGossip())
	require.True(tb, ts.syncer.ListenToGossip())
	require.True(tb, ts.syncer.IsSynced(context.Background()))

	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)

	require.True(tb, ts.syncer.synchronize(context.Background()))
	require.True(tb, ts.syncer.ListenToATXGossip())
	require.True(tb, ts.syncer.ListenToGossip())
	require.True(tb, ts.syncer.IsSynced(context.Background()))
	return current
}

func TestSyncAtxs_Genesis(t *testing.T) {
	t.Run("no atx expected", func(t *testing.T) {
		ts := newSyncerWithoutPeriodicRuns(t)
		ts.mTicker.advanceToLayer(1)
		require.True(t, ts.syncer.synchronize(context.Background()))
		require.True(t, ts.syncer.ListenToATXGossip())
		require.Equal(t, types.EpochID(0), ts.syncer.lastAtxEpoch())
	})
	t.Run("first atx epoch", func(t *testing.T) {
		ts := newSyncerWithoutPeriodicRuns(t)
		ts.expectMalDownloadLoop()
		epoch := types.EpochID(1)
		current := epoch.FirstLayer() + 2
		ts.mTicker.advanceToLayer(current) // to pass epoch end fraction threshold
		require.False(t, ts.syncer.ListenToATXGossip())
		wait := make(chan types.EpochID, 1)
		ts.mAtxSyncer.EXPECT().
			Download(gomock.Any(), epoch, gomock.Any()).
			DoAndReturn(func(ctx context.Context, epoch types.EpochID, _ time.Time) error {
				select {
				case wait <- epoch:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			})
		ts.expectMalEnsureInSync(current)
		require.True(t, ts.syncer.synchronize(context.Background()))
		require.True(t, ts.syncer.ListenToATXGossip())
		select {
		case downloaded := <-wait:
			require.Equal(t, epoch, downloaded)
		case <-time.After(time.Second):
			require.Fail(t, "timed out waiting for download of epoch %v", epoch)
		}
	})
}

func TestSyncAtxs_Genesis_SyncV2(t *testing.T) {
	cfg := defaultTestConfig(never)
	cfg.ReconcSync.Enable = true
	cfg.ReconcSync.EnableActiveSync = true

	t.Run("no atx expected", func(t *testing.T) {
		ts := newSyncerWithoutPeriodicRunsWithConfig(t, cfg)
		ts.mTicker.advanceToLayer(1)
		require.True(t, ts.syncer.synchronize(context.Background()))
		require.True(t, ts.syncer.ListenToATXGossip())
		require.Equal(t, types.EpochID(0), ts.syncer.lastAtxEpoch())
	})

	t.Run("first atx epoch", func(t *testing.T) {
		ts := newSyncerWithoutPeriodicRunsWithConfig(t, cfg)
		ts.expectMalDownloadLoop()
		epoch := types.EpochID(1)
		current := epoch.FirstLayer() + 2
		ts.mTicker.advanceToLayer(current) // to pass epoch end fraction threshold
		require.False(t, ts.syncer.ListenToATXGossip())
		ts.mASV2.EXPECT().EnsureSync(gomock.Any(), types.EpochID(0), epoch)
		ts.expectMalEnsureInSync(current)
		require.True(t, ts.syncer.synchronize(context.Background()))
		require.True(t, ts.syncer.ListenToATXGossip())
	})
}

func TestSyncAtxs(t *testing.T) {
	tcs := []struct {
		desc       string
		current    types.LayerID
		downloaded types.EpochID
	}{
		{
			desc:       "start of epoch",
			current:    13,
			downloaded: 3,
		},
		{
			desc:       "end of epoch",
			current:    14,
			downloaded: 4,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			ts := newSyncerWithoutPeriodicRuns(t)
			ts.expectMalDownloadLoop()
			lyr := startWithSyncedState(t, ts)
			require.LessOrEqual(t, lyr, tc.current)
			ts.mTicker.advanceToLayer(tc.current)

			ts.mAtxSyncer.EXPECT().Download(gomock.Any(), tc.downloaded, gomock.Any())
			for lid := lyr; lid < tc.current; lid++ {
				ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
			}
			require.True(t, ts.syncer.synchronize(context.Background()))
			ts.syncer.waitBackgroundSync()
			require.Equal(t, tc.downloaded, ts.syncer.lastAtxEpoch())
		})
	}
}

func startWithSyncedState_SyncV2(tb testing.TB, ts *testSyncer) types.LayerID {
	tb.Helper()

	gLayer := types.GetEffectiveGenesis()
	ts.mTicker.advanceToLayer(gLayer)
	ts.expectMalEnsureInSync(gLayer)
	ts.mASV2.EXPECT().EnsureSync(gomock.Any(), types.EpochID(0), types.EpochID(1)).MinTimes(1)
	require.True(tb, ts.syncer.synchronize(context.Background()))
	ts.syncer.waitBackgroundSync()
	require.True(tb, ts.syncer.ListenToATXGossip())
	require.True(tb, ts.syncer.ListenToGossip())
	require.True(tb, ts.syncer.IsSynced(context.Background()))

	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)

	require.True(tb, ts.syncer.synchronize(context.Background()))
	require.True(tb, ts.syncer.ListenToATXGossip())
	require.True(tb, ts.syncer.ListenToGossip())
	require.True(tb, ts.syncer.IsSynced(context.Background()))
	return current
}

func TestSyncAtxs_SyncV2(t *testing.T) {
	cfg := defaultTestConfig(never)
	cfg.ReconcSync.Enable = true
	cfg.ReconcSync.EnableActiveSync = true
	tcs := []struct {
		desc       string
		current    types.LayerID
		downloaded types.EpochID
	}{
		{
			desc:       "start of epoch",
			current:    13,
			downloaded: 3,
		},
		{
			desc:       "end of epoch",
			current:    14,
			downloaded: 4,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			ts := newSyncerWithoutPeriodicRunsWithConfig(t, cfg)
			ts.expectMalDownloadLoop()
			lyr := startWithSyncedState_SyncV2(t, ts)
			require.LessOrEqual(t, lyr, tc.current)
			ts.mTicker.advanceToLayer(tc.current)

			ts.mASV2.EXPECT().EnsureSync(gomock.Any(), types.EpochID(0), tc.downloaded)
			for lid := lyr; lid < tc.current; lid++ {
				ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
			}
			require.True(t, ts.syncer.synchronize(context.Background()))
		})
	}
}

func TestSynchronize_StaySyncedUponFailure(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), current.GetEpoch(), gomock.Any())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("doh"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.False(t, ts.syncer.dataSynced())
	ts.syncer.waitBackgroundSync()
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.Background()))
}

func TestSynchronize_BecomeNotSyncedUponFailureIfNoGossip(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("boo"))

	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), current.GetEpoch()-1, gomock.Any())

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.False(t, ts.syncer.dataSynced())
	ts.syncer.waitBackgroundSync()
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))
}

// test the case where the node originally starts from notSynced and eventually becomes synced.
func TestFromNotSyncedToSynced(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(5)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("baa-ram-ewe"))
	ts.expectMalEnsureInSync(current)

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
	}
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())

	advanceState(t, ts, lyr, current-1)
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	waitOutGossipSync(t, ts)
}

// test the case where the node originally starts from notSynced, advances to gossipSync, but falls behind
// to notSynced.
func TestFromGossipSyncToNotSynced(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.expectMalEnsureInSync(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())

	advanceState(t, ts, lyr, lyr)
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	lyr = lyr.Add(1)
	current = current.Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("baa-ram-ewe"))
	require.False(t, ts.syncer.synchronize(context.Background()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))
}

func TestNetworkHasNoData(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	lyr := startWithSyncedState(t, ts)
	require.True(t, ts.syncer.IsSynced(context.Background()))

	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	for lid := lyr.Add(1); lid < lyr.Add(outOfSyncThreshold+1); lid++ {
		ts.mTicker.advanceToLayer(lid)
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), gomock.Any())
		require.True(t, ts.syncer.synchronize(context.Background()))
		require.True(t, ts.syncer.dataSynced())
		require.True(t, ts.syncer.ListenToATXGossip())
		require.True(t, ts.syncer.ListenToGossip())
		require.True(t, ts.syncer.IsSynced(context.Background()))
	}
	// the network hasn't received any data
	require.Greater(
		t,
		int(ts.syncer.ticker.CurrentLayer()-ts.msh.LatestLayer()),
		outOfSyncThreshold,
	)
}

// test the case where the node was originally synced, and somehow gets out of sync, but
// eventually become synced again.
func TestFromSyncedToNotSynced(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.IsSynced(context.Background()))

	// cause the syncer to get out of synced and then wait again
	lyr := types.GetEffectiveGenesis().Add(1)
	current := ts.msh.LatestLayer().Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("baa-ram-ewe"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
	}
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())

	advanceState(t, ts, lyr, current-1)
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	waitOutGossipSync(t, ts)
}

func waitOutGossipSync(tb testing.TB, ts *testSyncer) {
	require.True(tb, ts.syncer.dataSynced())
	require.True(tb, ts.syncer.ListenToATXGossip())
	require.True(tb, ts.syncer.ListenToGossip())
	require.False(tb, ts.syncer.IsSynced(context.Background()))

	// next layer will be still gossip syncing
	require.Eventually(tb, func() bool {
		require.True(tb, ts.syncer.synchronize(context.Background()))
		return ts.syncer.IsSynced(context.Background())
	}, time.Second, 100*time.Millisecond)
}

func TestSync_AlsoSyncProcessedLayer(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)

	ts.expectMalDownloadLoop()
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.expectMalEnsureInSync(current)

	// simulate hare advancing the mesh forward
	ts.mTortoise.EXPECT().TallyVotes(lyr)
	ts.mTortoise.EXPECT().Updates().Return(fixture.RLayers(fixture.RLayer(lyr)))
	ts.mTortoise.EXPECT().OnApplied(lyr, gomock.Any())
	ts.mVm.EXPECT().Apply(gomock.Any(), nil, nil)
	ts.mConState.EXPECT().UpdateCache(gomock.Any(), lyr, types.EmptyBlockID, nil, nil)
	ts.mVm.EXPECT().GetStateRoot()
	ts.mTortoise.EXPECT().OnHareOutput(lyr, types.EmptyBlockID)
	require.NoError(
		t,
		ts.msh.ProcessLayerPerHareOutput(context.Background(), lyr, types.EmptyBlockID, false),
	)
	require.Equal(t, lyr, ts.msh.ProcessedLayer())

	// no data sync should happen
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)
	require.True(t, ts.syncer.synchronize(context.Background()))
	// but last synced is updated
	require.Equal(t, lyr, ts.syncer.getLastSyncedLayer())
}

func TestSyncer_setATXSyncedTwice_NoError(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)

	atxSync := ts.syncer.RegisterForATXSynced()
	select {
	case <-atxSync:
		require.Fail(t, "should not have reached synced state yet")
	case <-time.After(100 * time.Millisecond):
	}

	ts.syncer.setATXSynced()

	select {
	case <-atxSync:
	case <-time.After(1 * time.Second):
		require.Fail(t, "should have reached synced state")
	}

	require.NotPanics(t, func() { ts.syncer.setATXSynced() })
}

func TestSynchronize_RecoverFromCheckpoint(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.expectMalDownloadLoop()
	current := types.GetEffectiveGenesis().Add(types.GetLayersPerEpoch() * 5)
	// recover from a checkpoint
	types.SetEffectiveGenesis(current.Uint32())
	ts.mTicker.advanceToLayer(current)
	var err error
	ts.syncer, err = NewSyncer(
		ts.cdb,
		ts.mTicker,
		ts.msh,
		ts.mTortoise,
		nil,
		nil,
		nil,
		ts.mLyrPatrol,
		ts.mCertHdr,
		ts.mAtxSyncer,
		ts.mMalSyncer,
		WithConfig(ts.syncer.cfg),
		WithLogger(ts.syncer.logger),
		withDataFetcher(ts.mDataFetcher),
		withForkFinder(ts.mForkFinder),
		withAtxSyncerV2(ts.mASV2),
	)
	require.NoError(t, err)
	// should not sync any atxs before current epoch
	ts.mAtxSyncer.EXPECT().Download(gomock.Any(), current.GetEpoch(), gomock.Any())

	ts.expectMalEnsureInSync(current)
	require.True(t, ts.syncer.synchronize(context.Background()))
	ts.syncer.waitBackgroundSync()
	require.Equal(t, current.GetEpoch(), ts.syncer.lastAtxEpoch())
	types.SetEffectiveGenesis(types.FirstEffectiveGenesis().Uint32())
}

func TestSyncBeforeGenesis(t *testing.T) {
	ts := newSyncerWithoutPeriodicRuns(t)
	ts.mTicker.advanceToLayer(0)
	require.False(t, ts.syncer.synchronize(context.Background()))
	select {
	case <-ts.syncer.RegisterForATXSynced():
	default:
		require.Fail(t, "should consider atxs to be synced")
	}
	require.True(t, ts.syncer.IsSynced(context.Background()))
}
