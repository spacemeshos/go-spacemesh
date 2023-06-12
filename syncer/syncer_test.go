package syncer

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/fixture"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	mmocks "github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	layersPerEpoch = 3
	never          = time.Second * 60 * 24
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

type mockLayerTicker struct {
	current atomic.Value
}

func newMockLayerTicker() *mockLayerTicker {
	mt := &mockLayerTicker{}
	mt.current.Store(types.LayerID(1))
	return mt
}

func (mlt *mockLayerTicker) advanceToLayer(layerID types.LayerID) {
	mlt.current.Store(layerID)
}

func (mlt *mockLayerTicker) CurrentLayer() types.LayerID {
	return mlt.current.Load().(types.LayerID)
}

type testSyncer struct {
	syncer  *Syncer
	cdb     *datastore.CachedDB
	msh     *mesh.Mesh
	mTicker *mockLayerTicker

	mDataFetcher *mocks.MockfetchLogic
	mBeacon      *smocks.MockBeaconGetter
	mLyrPatrol   *mocks.MocklayerPatrol
	mVm          *mmocks.MockvmState
	mConState    *mmocks.MockconservativeState
	mTortoise    *smocks.MockTortoise
	mCertHdr     *mocks.MockcertHandler
	mForkFinder  *mocks.MockforkFinder
}

func newTestSyncer(t *testing.T, interval time.Duration) *testSyncer {
	lg := logtest.New(t)
	mt := newMockLayerTicker()
	ctrl := gomock.NewController(t)
	ts := &testSyncer{
		mTicker:      mt,
		mDataFetcher: mocks.NewMockfetchLogic(ctrl),
		mBeacon:      smocks.NewMockBeaconGetter(ctrl),
		mLyrPatrol:   mocks.NewMocklayerPatrol(ctrl),
		mVm:          mmocks.NewMockvmState(ctrl),
		mConState:    mmocks.NewMockconservativeState(ctrl),
		mTortoise:    smocks.NewMockTortoise(ctrl),
		mCertHdr:     mocks.NewMockcertHandler(ctrl),
		mForkFinder:  mocks.NewMockforkFinder(ctrl),
	}
	ts.cdb = datastore.NewCachedDB(sql.InMemory(), lg)
	var err error
	exec := mesh.NewExecutor(ts.cdb, ts.mVm, ts.mConState, lg)
	ts.msh, err = mesh.NewMesh(ts.cdb, ts.mTicker, ts.mTortoise, exec, ts.mConState, lg)
	require.NoError(t, err)

	cfg := Config{
		Interval:         interval,
		EpochEndFraction: 0.66,
		SyncCertDistance: 4,
		HareDelayLayers:  5,
	}
	ts.syncer = NewSyncer(ts.cdb, ts.mTicker, ts.mBeacon, ts.msh, nil, nil, ts.mLyrPatrol, ts.mCertHdr,
		WithConfig(cfg),
		WithLogger(lg),
		withDataFetcher(ts.mDataFetcher),
		withForkFinder(ts.mForkFinder))
	ts.mDataFetcher.EXPECT().GetPeers().Return([]p2p.Peer{"non-empty"}).AnyTimes()
	return ts
}

func newSyncerWithoutSyncTimer(t *testing.T) *testSyncer {
	ts := newTestSyncer(t, never)
	ts.syncer.syncTimer.Stop()
	ts.syncer.validateTimer.Stop()
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
	ts.syncer.Start(ctx)

	ts.mForkFinder.EXPECT().Purge(false).AnyTimes()
	ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), gomock.Any()).AnyTimes()
	require.Eventually(t, func() bool {
		return ts.syncer.ListenToATXGossip() && ts.syncer.ListenToGossip() && ts.syncer.IsSynced(ctx)
	}, time.Second, 10*time.Millisecond)

	cancel()
	require.False(t, ts.syncer.synchronize(ctx))
	ts.syncer.Close()
}

func TestSynchronize_OnlyOneSynchronize(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	current := types.LayerID(10)
	ts.mTicker.advanceToLayer(current)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts.syncer.Start(ctx)

	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
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
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		require.True(t, ts.syncer.synchronize(ctx))
		wg.Done()
	}()
	<-started
	require.False(t, ts.syncer.synchronize(ctx))
	// allow synchronize to finish
	close(done)
	wg.Wait()

	cancel()
	ts.syncer.Close()
}

func TestSynchronize_AllGood(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(10)
	ts.mTicker.advanceToLayer(current)
	for epoch := gLayer.GetEpoch(); epoch <= current.GetEpoch(); epoch++ {
		ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch)
	}
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	for lid := gLayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			wg.Done()
		case <-time.After(1 * time.Second):
			require.Fail(t, "node should be atx synced")
		}
	}()

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, current.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current.GetEpoch(), ts.syncer.lastAtxEpoch())
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	wg.Add(1)
	go func() {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			wg.Done()
		case <-time.After(1 * time.Second):
			require.Fail(t, "node should be atx synced")
		}
	}()
	wg.Wait()
}

func TestSynchronize_FetchLayerDataFailed(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gLayer.GetEpoch())
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gLayer.GetEpoch()+1)
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("meh"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, lyr.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current.GetEpoch(), ts.syncer.lastAtxEpoch())
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))
}

func TestSynchronize_FetchMalfeasanceFailed(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any()).Return(errors.New("meh"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.EqualValues(t, current.GetEpoch(), ts.syncer.lastAtxEpoch())
	require.Equal(t, lyr.Sub(1), ts.syncer.getLastSyncedLayer())
}

func TestSynchronize_FailedInitialATXsSync(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	failedEpoch := types.EpochID(4)
	current := types.LayerID(layersPerEpoch * uint32(failedEpoch+1))
	ts.mTicker.advanceToLayer(current)
	for epoch := types.GetEffectiveGenesis().GetEpoch(); epoch < failedEpoch; epoch++ {
		ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch)
	}
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), failedEpoch).Return(errors.New("no ATXs. should fail sync"))

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			require.Fail(t, "node should not be atx synced")
		case <-time.After(100 * time.Millisecond):
			wg.Done()
		}
	}()

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	require.Equal(t, failedEpoch-1, ts.syncer.lastAtxEpoch())
	require.False(t, ts.syncer.dataSynced())
	require.False(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	wg.Add(1)
	go func() {
		atxSyncedCh := ts.syncer.RegisterForATXSynced()
		select {
		case <-atxSyncedCh:
			require.Fail(t, "node should not be atx synced")
		case <-time.After(100 * time.Millisecond):
			wg.Done()
		}
	}()
	wg.Wait()
}

func startWithSyncedState(t *testing.T, ts *testSyncer) types.LayerID {
	t.Helper()

	gLayer := types.GetEffectiveGenesis()
	ts.mTicker.advanceToLayer(gLayer)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gLayer.GetEpoch())
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.Background()))

	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.Background()))
	return current
}

func TestSyncAtxs_Genesis(t *testing.T) {
	tcs := []struct {
		desc              string
		epoch, lastSynced types.EpochID
	}{
		{
			desc:       "no atx expected",
			epoch:      0,
			lastSynced: 0,
		},
		{
			desc:       "first atx epoch",
			epoch:      1,
			lastSynced: 1,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			ts := newSyncerWithoutSyncTimer(t)
			ts.mTicker.advanceToLayer(tc.epoch.FirstLayer() + 1)
			if tc.lastSynced > 0 {
				require.False(t, ts.syncer.ListenToATXGossip())
				for epoch := types.EpochID(1); epoch <= tc.lastSynced; epoch++ {
					ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch)
				}
			}
			require.True(t, ts.syncer.synchronize(context.Background()))
			require.True(t, ts.syncer.ListenToATXGossip())
			require.Equal(t, tc.lastSynced, ts.syncer.lastAtxEpoch())
		})
	}
}

func TestSyncAtxs(t *testing.T) {
	tcs := []struct {
		desc          string
		current       types.LayerID
		lastSyncEpoch types.EpochID
	}{
		{
			desc:          "start of epoch",
			current:       7,
			lastSyncEpoch: 1,
		},
		{
			desc:          "end of epoch",
			current:       8,
			lastSyncEpoch: 2,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			ts := newSyncerWithoutSyncTimer(t)
			lyr := startWithSyncedState(t, ts)
			require.LessOrEqual(t, lyr, tc.current)

			require.Equal(t, lyr.GetEpoch()-1, ts.syncer.lastAtxEpoch())
			ts.mTicker.advanceToLayer(tc.current)
			for epoch := lyr.GetEpoch(); epoch <= tc.lastSyncEpoch; epoch++ {
				ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch)
			}
			for lid := lyr; lid < tc.current; lid++ {
				ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
			}

			require.True(t, ts.syncer.synchronize(context.Background()))
			require.Equal(t, tc.lastSyncEpoch, ts.syncer.lastAtxEpoch())
		})
	}
}

func TestSynchronize_StaySyncedUponFailure(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("doh"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.Background()))
}

func TestSynchronize_BecomeNotSyncedUponFailureIfNoGossip(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("boo"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))
}

// test the case where the node originally starts from notSynced and eventually becomes synced.
func TestFromNotSyncedToSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(5)
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
	// node should be in gossip sync state
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	waitOutGossipSync(t, current, ts)
}

// test the case where the node originally starts from notSynced, advances to gossipSync, but falls behind
// to notSynced.
func TestFromGossipSyncToNotSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)

	require.True(t, ts.syncer.synchronize(context.Background()))
	// node should be in gossip sync state
	require.True(t, ts.syncer.dataSynced())
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

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
	}
	require.True(t, ts.syncer.synchronize(context.Background()))
	// the node should enter gossipSync again
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	waitOutGossipSync(t, current, ts)
}

func TestNetworkHasNoData(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)
	require.True(t, ts.syncer.IsSynced(context.Background()))

	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
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
	require.Greater(t, ts.syncer.ticker.CurrentLayer()-ts.msh.LatestLayer(), outOfSyncThreshold)
}

// test the case where the node was originally synced, and somehow gets out of sync, but
// eventually become synced again.
func TestFromSyncedToNotSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any()).AnyTimes()

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
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	waitOutGossipSync(t, current, ts)
}

func waitOutGossipSync(t *testing.T, current types.LayerID, ts *testSyncer) {
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	// next layer will be still gossip syncing
	require.Equal(t, types.LayerID(2).Uint32(), numGossipSyncLayers)
	require.Equal(t, current.Add(numGossipSyncLayers), ts.syncer.getTargetSyncedLayer())

	lyr := current
	current = current.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	// done one full layer of gossip sync, now it is synced
	lyr = lyr.Add(1)
	current = current.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.Background()))
}

func TestSync_AlsoSyncProcessedLayer(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)

	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)

	// simulate hare advancing the mesh forward
	ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lyr)
	ts.mTortoise.EXPECT().Updates().Return(fixture.RLayers(fixture.RLayer(lyr)))
	ts.mVm.EXPECT().Apply(gomock.Any(), nil, nil)
	ts.mConState.EXPECT().UpdateCache(gomock.Any(), lyr, types.EmptyBlockID, nil, nil)
	ts.mVm.EXPECT().GetStateRoot()
	ts.mTortoise.EXPECT().OnHareOutput(lyr, types.EmptyBlockID)
	require.NoError(t, ts.msh.ProcessLayerPerHareOutput(context.Background(), lyr, types.EmptyBlockID, false))
	require.Equal(t, lyr, ts.msh.ProcessedLayer())

	// no data sync should happen
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr)
	require.True(t, ts.syncer.synchronize(context.Background()))
	// but last synced is updated
	require.Equal(t, lyr, ts.syncer.getLastSyncedLayer())
}

func TestSyncer_setATXSyncedTwice_NoError(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)

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

func TestSyncer_IsBeaconSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	epoch := types.EpochID(11)
	ts.mBeacon.EXPECT().GetBeacon(epoch).Return(types.EmptyBeacon, errors.New("unknown"))
	require.False(t, ts.syncer.IsBeaconSynced(epoch))
	ts.mBeacon.EXPECT().GetBeacon(epoch).Return(types.RandomBeacon(), nil)
	require.True(t, ts.syncer.IsBeaconSynced(epoch))
}

func TestSynchronize_RecoverFromCheckpoint(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	current := types.GetEffectiveGenesis().Add(types.GetLayersPerEpoch() * 5)
	// recover from a checkpoint
	types.SetEffectiveGenesis(current.Uint32())
	ts.mTicker.advanceToLayer(current)
	ts.syncer = NewSyncer(ts.cdb, ts.mTicker, ts.mBeacon, ts.msh, nil, nil, ts.mLyrPatrol, ts.mCertHdr,
		WithConfig(ts.syncer.cfg),
		WithLogger(ts.syncer.logger),
		withDataFetcher(ts.mDataFetcher),
		withForkFinder(ts.mForkFinder))
	// should not sync any atxs before current epoch
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch())
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, current.GetEpoch(), ts.syncer.lastAtxEpoch())
	types.SetEffectiveGenesis(types.FirstEffectiveGenesis().Uint32())
}
