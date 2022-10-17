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

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
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

	mDataFetcher *mocks.MockdataFetcher
	mBeacon      *smocks.MockBeaconGetter
	mLyrPatrol   *mocks.MocklayerPatrol
	mConState    *mmocks.MockconservativeState
	mTortoise    *smocks.MockTortoise
	mCertHdr     *mocks.MockcertHandler
}

func newTestSyncer(ctx context.Context, t *testing.T, interval time.Duration) *testSyncer {
	lg := logtest.New(t)

	mt := newMockLayerTicker()
	ctrl := gomock.NewController(t)
	mcs := mmocks.NewMockconservativeState(ctrl)
	mtrt := smocks.NewMockTortoise(ctrl)
	mb := smocks.NewMockBeaconGetter(ctrl)
	cdb := datastore.NewCachedDB(sql.InMemory(), lg)
	mm, err := mesh.NewMesh(cdb, mtrt, mcs, lg)
	require.NoError(t, err)

	mp := mocks.NewMocklayerPatrol(ctrl)
	mf := mocks.NewMockdataFetcher(ctrl)
	cfg := Config{
		SyncInterval:     interval,
		SyncCertDistance: 4,
		HareDelayLayers:  5,
	}
	mc := mocks.NewMockcertHandler(ctrl)
	return &testSyncer{
		syncer: NewSyncer(cdb, mt, mb, mm, nil, mp, mc,
			WithContext(ctx),
			WithConfig(cfg),
			WithLogger(lg),
			withDataFetcher(mf)),
		cdb:          cdb,
		msh:          mm,
		mTicker:      mt,
		mDataFetcher: mf,
		mBeacon:      mb,
		mLyrPatrol:   mp,
		mConState:    mcs,
		mTortoise:    mtrt,
		mCertHdr:     mc,
	}
}

func newSyncerWithoutSyncTimer(t *testing.T) *testSyncer {
	ctx := context.TODO()
	ts := newTestSyncer(ctx, t, never)
	ts.syncer.syncTimer.Stop()
	ts.syncer.validateTimer.Stop()
	return ts
}

func TestStartAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	ts := newTestSyncer(ctx, t, time.Millisecond*5)

	require.False(t, ts.syncer.IsSynced(context.TODO()))
	require.False(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())

	// the node is synced when current layer is <= 1
	ts.syncer.Start(context.TODO())

	require.Eventually(t, func() bool {
		return ts.syncer.ListenToATXGossip() && ts.syncer.ListenToGossip() && ts.syncer.IsSynced(context.TODO())
	}, time.Second, 10*time.Millisecond)

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

	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	gLayer := types.GetEffectiveGenesis()

	started := make(chan struct{}, 1)
	done := make(chan struct{}, 1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), gLayer.Add(1)).DoAndReturn(
		func(context.Context, types.LayerID) error {
			close(started)
			<-done
			return nil
		},
	)
	for lid := gLayer.Add(2); lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).Return(nil)
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
	close(done)
	wg.Wait()

	require.Equal(t, uint64(1), ts.syncer.run)
	ts.syncer.Close()
}

func TestSynchronize_AllGood(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	gLayer := types.GetEffectiveGenesis()
	current := gLayer.Add(10)
	ts.mTicker.advanceToLayer(current)
	for epoch := gLayer.GetEpoch(); epoch <= current.GetEpoch(); epoch++ {
		ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch).Return(nil)
	}
	for lid := gLayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).Return(nil)
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

	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.Equal(t, current.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current.GetEpoch(), ts.syncer.getLastSyncedATXs())
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

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
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gLayer.GetEpoch()).Return(nil)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch()).Return(nil)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("meh"))

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
		ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), epoch).Return(nil)
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

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	require.Equal(t, failedEpoch-1, ts.syncer.getLastSyncedATXs())
	require.False(t, ts.syncer.dataSynced())
	require.False(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

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
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gLayer.GetEpoch()).Return(nil)
	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) error {
			ts.msh.SetZeroBlockLayer(context.TODO(), got)
			return nil
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
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch()-1).Return(nil)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), gomock.Any()).Return(nil).Times(2)

	require.True(t, ts.syncer.synchronize(context.TODO()))
}

func TestSynchronize_StaySyncedUponFailure(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("doh"))

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
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("boo"))

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))
}

// test the case where the node originally starts from notSynced and eventually becomes synced.
func TestFromNotSyncedToSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(5)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("baa-ram-ewe"))

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID) error {
				ts.msh.SetZeroBlockLayer(context.TODO(), got)
				return nil
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
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) error {
			ts.msh.SetZeroBlockLayer(context.TODO(), got)
			return nil
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
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("baa-ram-ewe"))
	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID) error {
				ts.msh.SetZeroBlockLayer(context.TODO(), got)
				return nil
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
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.IsSynced(context.TODO()))

	// cause the syncer to get out of synced and then wait again
	lyr := types.GetEffectiveGenesis().Add(1)
	current := ts.msh.LatestLayer().Add(outOfSyncThreshold)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("baa-ram-ewe"))

	require.False(t, ts.syncer.synchronize(context.TODO()))
	require.False(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.False(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	for lid := lyr; lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID) error {
				ts.msh.SetZeroBlockLayer(context.TODO(), got)
				return nil
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
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) error {
			ts.msh.SetZeroBlockLayer(context.TODO(), got)
			return nil
		})
	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.TODO()))

	// done one full layer of gossip sync, now it is synced
	lyr = lyr.Add(1)
	current = current.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID) error {
			ts.msh.SetZeroBlockLayer(context.TODO(), got)
			return nil
		})
	require.True(t, ts.syncer.synchronize(context.TODO()))
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.TODO()))
}

func TestSyncMissingLayer(t *testing.T) {
	ts := newTestSyncer(context.TODO(), t, never)
	genesis := types.GetEffectiveGenesis()
	failed := genesis.Add(2)
	last := genesis.Add(4)
	ts.mTicker.advanceToLayer(last)

	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
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
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).Return(nil)
		ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(nil, nil)
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
		if lid.Before(failed) {
			ts.msh.SetZeroBlockLayer(context.TODO(), lid)
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
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), failed).Return(nil)
	require.True(t, ts.syncer.synchronize(context.TODO()))

	for lid := failed; lid.Before(last); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(nil, nil)
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
		if lid == failed {
			ts.mConState.EXPECT().ApplyLayer(gomock.Any(), block).DoAndReturn(
				func(_ context.Context, got *types.Block) error {
					require.Equal(t, block.ID(), got.ID())
					return nil
				})
		} else {
			ts.msh.SetZeroBlockLayer(context.TODO(), lid)
		}
	}
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.Equal(t, types.LayerID{}, ts.msh.MissingLayer())
	require.Equal(t, last.Sub(1), ts.msh.ProcessedLayer())
	require.Equal(t, last.Sub(1), ts.msh.LatestLayerInState())
}

func TestSync_AlsoSyncProcessedLayer(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	lyr := types.GetEffectiveGenesis().Add(1)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)

	// simulate hare advancing the mesh forward
	ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lyr)
	ts.mTortoise.EXPECT().LatestComplete().Return(lyr.Sub(1))
	ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	ts.mTortoise.EXPECT().OnHareOutput(lyr, types.EmptyBlockID)
	require.NoError(t, ts.msh.ProcessLayerPerHareOutput(context.TODO(), lyr, types.EmptyBlockID))
	require.Equal(t, lyr, ts.msh.ProcessedLayer())

	// no data sync should happen
	require.Equal(t, types.GetEffectiveGenesis(), ts.syncer.getLastSyncedLayer())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(nil)
	require.True(t, ts.syncer.synchronize(context.TODO()))
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
