package syncer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	mmocks "github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	layersPerEpoch = 3
)

func init() {
	types.SetLayersPerEpoch(layersPerEpoch)
}

type mockLayerTicker struct {
	now            time.Time
	current        unsafe.Pointer
	layerStartTime time.Time
}

func newMockLayerTicker() *mockLayerTicker {
	firstLayer := types.NewLayerID(1)
	return &mockLayerTicker{current: unsafe.Pointer(&firstLayer)}
}

func (mlt *mockLayerTicker) advanceToLayer(layerID types.LayerID) {
	atomic.StorePointer(&mlt.current, unsafe.Pointer(&layerID))
}

func (mlt *mockLayerTicker) GetCurrentLayer() types.LayerID {
	return *(*types.LayerID)(atomic.LoadPointer(&mlt.current))
}

func (mlt *mockLayerTicker) LayerToTime(_ types.LayerID) time.Time {
	return mlt.layerStartTime
}

type mockFetcher struct {
	mu        sync.Mutex
	polled    map[types.LayerID]chan struct{}
	result    map[types.LayerID]chan layerfetcher.LayerPromiseResult
	atxsError map[types.EpochID]error
	atxsCalls uint32
}

func newMockFetcher() *mockFetcher {
	numLayers := layersPerEpoch * 5
	polled := make(map[types.LayerID]chan struct{}, numLayers)
	result := make(map[types.LayerID]chan layerfetcher.LayerPromiseResult, numLayers)
	for i := uint32(0); i <= uint32(numLayers); i++ {
		polled[types.NewLayerID(i)] = make(chan struct{}, 10)
		result[types.NewLayerID(i)] = make(chan layerfetcher.LayerPromiseResult, 10)
	}
	return &mockFetcher{result: result, polled: polled, atxsError: make(map[types.EpochID]error)}
}

func (mf *mockFetcher) PollLayerContent(_ context.Context, layerID types.LayerID) chan layerfetcher.LayerPromiseResult {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.polled[layerID] <- struct{}{}
	return mf.result[layerID]
}

func (mf *mockFetcher) GetEpochATXs(_ context.Context, epoch types.EpochID) error {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.atxsCalls++
	return mf.atxsError[epoch]
}

func (mf *mockFetcher) getLayerPollChan(layerID types.LayerID) chan struct{} {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	return mf.polled[layerID]
}

func (mf *mockFetcher) getLayerResultChan(layerID types.LayerID) chan layerfetcher.LayerPromiseResult {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	return mf.result[layerID]
}

func (mf *mockFetcher) setATXsErrors(epoch types.EpochID, err error) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.atxsError[epoch] = err
}

type mockValidator struct{}

func (mv *mockValidator) OnBlock(*types.Block) {}

func (mv *mockValidator) OnBallot(*types.Ballot) {}

func (mv *mockValidator) HandleIncomingLayer(_ context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	return layerID, layerID.Sub(1), false
}

func feedLayerResult(from, to types.LayerID, mf *mockFetcher, msh *mesh.Mesh) {
	for i := from; !i.After(to); i = i.Add(1) {
		msh.SetZeroBlockLayer(i)
		mf.getLayerResultChan(i) <- layerfetcher.LayerPromiseResult{
			Layer: i,
			Err:   nil,
		}
	}
}

func newMemMesh(t *testing.T, lg log.Log) *mesh.Mesh {
	memdb := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxStore := database.NewMemDatabase()
	goldenATXID := types.ATXID(types.HexToHash32("77777"))
	atxdb := activation.NewDB(atxStore, nil,
		activation.NewIdentityStore(database.NewMemDatabase()),
		layersPerEpoch, goldenATXID, nil, lg.WithName("atxDB"))
	svmstate := mmocks.NewMockstate(gomock.NewController(t))
	svmstate.EXPECT().GetStateRoot().AnyTimes()
	return mesh.NewMesh(memdb, atxdb, &mockValidator{}, nil, svmstate, lg.WithName("mesh"))
}

var conf = Configuration{
	SyncInterval: time.Second * 60 * 60 * 24, // long enough that it doesn't kick in during testing
	AlwaysListen: false,
}

func newSyncer(ctx context.Context, t *testing.T, conf Configuration, ticker layerTicker, mesh *mesh.Mesh, fetcher layerFetcher, logger log.Log) *Syncer {
	ctrl := gomock.NewController(t)
	beacons := smocks.NewMockBeaconGetter(ctrl)
	beacons.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil).AnyTimes()
	patrol := mocks.NewMocklayerPatrol(ctrl)
	patrol.EXPECT().IsHareInCharge(gomock.Any()).Return(false).AnyTimes()
	return NewSyncer(ctx, conf, ticker, beacons, mesh, fetcher, patrol, logger)
}

func newSyncerWithoutSyncTimer(ctx context.Context, t *testing.T, conf Configuration, ticker layerTicker, mesh *mesh.Mesh, fetcher layerFetcher, logger log.Log) *Syncer {
	syncer := newSyncer(ctx, t, conf, ticker, mesh, fetcher, logger)
	syncer.syncTimer.Stop()
	return syncer
}

func TestStartAndShutdown(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	ctx, cancel := context.WithCancel(context.TODO())
	conf.SyncInterval = time.Millisecond * 5
	syncer := newSyncer(ctx, t, conf, ticker, newMemMesh(t, lg), newMockFetcher(), lg)
	syncedCh := make(chan struct{})
	syncer.RegisterChForSynced(context.TODO(), syncedCh)

	assert.False(t, syncer.IsSynced(context.TODO()))
	assert.False(t, syncer.ListenToGossip())

	// the node is synced when current layer is <= 1
	syncer.Start(context.TODO())
	<-syncedCh
	assert.True(t, syncer.IsSynced(context.TODO()))
	assert.True(t, syncer.ListenToGossip())

	cancel()
	assert.True(t, syncer.isClosed())
	assert.False(t, syncer.synchronize(context.TODO()))
	syncer.Close()
}

func TestSynchronize_OnlyOneSynchronize(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(types.NewLayerID(10))
	syncer.Start(context.TODO())

	var wg sync.WaitGroup
	wg.Add(2)
	first, second := true, true
	started := make(chan struct{}, 2)
	go func() {
		started <- struct{}{}
		first = syncer.synchronize(context.TODO())
		wg.Done()
	}()
	<-started
	go func() {
		started <- struct{}{}
		second = syncer.synchronize(context.TODO())
		wg.Done()
	}()
	<-started
	// allow synchronize to finish
	current := ticker.GetCurrentLayer()
	feedLayerResult(types.GetEffectiveGenesis().Add(1), current.Sub(1), mf, mm)
	wg.Wait()

	// one of the synchronize calls should fail
	assert.False(t, first && second)
	assert.Equal(t, uint64(1), syncer.run)
	syncer.Close()
}

func TestSynchronize_AllGood(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(10)
	ticker.advanceToLayer(current)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow synchronize to finish
	feedLayerResult(glayer.Add(1), current.Sub(1), mf, mm)
	wg.Wait()

	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_ValidationDoneAfterCurrentAdvanced(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	validator := mocks.NewMocklayerValidator(ctrl)
	syncer.validator = validator

	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(8)
	ticker.advanceToLayer(current)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	arrivedOldCurrent := make(chan struct{}, 1)
	finishOldCurrent := make(chan struct{}, 1)
	newCurrent := current.Add(2)
	for l := glayer.Add(1); l.Before(newCurrent); l = l.Add(1) {
		l := l
		validator.EXPECT().ProcessLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, layerID types.LayerID) error {
				assert.Equal(t, l, layerID)
				if l == current.Sub(1) {
					arrivedOldCurrent <- struct{}{}
					<-finishOldCurrent
				}
				// cause mesh's processed layer to advance
				mm.ProcessLayerPerHareOutput(ctx, l, types.EmptyBlockID)
				return nil
			}).Times(1)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow data sync to finish
	feedLayerResult(glayer.Add(1), newCurrent.Sub(1), mf, mm)
	// now advance current further
	<-arrivedOldCurrent
	ticker.advanceToLayer(newCurrent)
	finishOldCurrent <- struct{}{}
	wg.Wait()

	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_MaxAttemptWithinRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	validator := mocks.NewMocklayerValidator(ctrl)
	syncer.validator = validator

	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(5)
	ticker.advanceToLayer(current)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	for l := glayer.Add(1); l.Before(current); l = l.Add(1) {
		l := l
		validator.EXPECT().ProcessLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, layerID types.LayerID) error {
				assert.Equal(t, l, layerID)
				// cause mesh's processed layer to advance
				mm.ProcessLayerPerHareOutput(ctx, l, types.EmptyBlockID)
				return nil
			}).Times(1)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow synchronize to finish
	feedLayerResult(glayer.Add(1), current.Sub(1), mf, mm)
	wg.Wait()

	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	oldTargetLayer := syncer.getTargetSyncedLayer()
	assert.Equal(t, current.Add(numGossipSyncLayers), oldTargetLayer)

	ticker.advanceToLayer(ticker.GetCurrentLayer().Add(1))
	lastLayer := current.Add(maxAttemptWithinRun)
	for l := current; l.Before(lastLayer); l = l.Add(1) {
		l := l
		validator.EXPECT().ProcessLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, layerID types.LayerID) error {
				assert.Equal(t, l, layerID)
				// cause mesh's processed layer to advance
				mm.ProcessLayerPerHareOutput(ctx, l, types.EmptyBlockID)
				// but also advance current layer
				ticker.advanceToLayer(ticker.GetCurrentLayer().Add(1))
				return nil
			}).Times(1)
	}

	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow synchronize to finish
	feedLayerResult(current, lastLayer.Sub(1), mf, mm)
	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	newTargetLayer := syncer.getTargetSyncedLayer()
	assert.Greater(t, newTargetLayer.Uint32(), oldTargetLayer.Uint32())
	assert.Equal(t, ticker.GetCurrentLayer().Add(numGossipSyncLayers), newTargetLayer)
}

func startWithSyncedState(t *testing.T, syncer *Syncer) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	wg.Wait()
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_BeaconDelay(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	startWithSyncedState(t, syncer)

	patrol := mocks.NewMocklayerPatrol(ctrl)
	syncer.patrol = patrol
	validator := mocks.NewMocklayerValidator(ctrl)
	syncer.validator = validator
	beacons := smocks.NewMockBeaconGetter(ctrl)
	syncer.beacons = beacons

	gLayer := types.GetEffectiveGenesis()
	lyr := gLayer.Add(3)
	for l := gLayer.Add(1); !l.After(gLayer.Add(2)); l = l.Add(1) {
		l := l
		beacons.EXPECT().GetBeacon(l.GetEpoch()).Return(l.GetEpoch().ToBytes(), nil).Times(1)
		patrol.EXPECT().IsHareInCharge(l).Return(false).Times(1)
		validator.EXPECT().ProcessLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, layerID types.LayerID) error {
				assert.Equal(t, l, layerID)
				// cause mesh's processed layer to advance
				mm.ProcessLayerPerHareOutput(ctx, l, types.EmptyBlockID)
				return nil
			}).Times(1)
	}
	patrol.EXPECT().IsHareInCharge(lyr).Return(false).Times(maxAttemptWithinRun)
	beacons.EXPECT().GetBeacon(lyr.GetEpoch()).Return(types.EmptyBeacon, database.ErrNotFound).Times(maxAttemptWithinRun)

	ticker.advanceToLayer(lyr.Add(1))
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	// allow synchronize to finish
	feedLayerResult(gLayer.Add(1), lyr, mf, mm)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_OnlyValidateSomeLayers(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	startWithSyncedState(t, syncer)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	patrol := mocks.NewMocklayerPatrol(ctrl)
	syncer.patrol = patrol
	validator := mocks.NewMocklayerValidator(ctrl)
	syncer.validator = validator

	gLayer := types.GetEffectiveGenesis()
	lyr := gLayer.Add(3)
	for l := gLayer.Add(1); !l.After(gLayer.Add(2)); l = l.Add(1) {
		l := l
		patrol.EXPECT().IsHareInCharge(l).Return(false).Times(1)
		validator.EXPECT().ProcessLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, layerID types.LayerID) error {
				assert.Equal(t, l, layerID)
				// cause mesh's processed layer to advance
				mm.ProcessLayerPerHareOutput(ctx, l, types.EmptyBlockID)
				return nil
			}).Times(1)
	}
	patrol.EXPECT().IsHareInCharge(lyr).Return(true).Times(maxAttemptWithinRun)

	ticker.advanceToLayer(lyr.Add(1))
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	// allow synchronize to finish
	feedLayerResult(gLayer.Add(1), lyr, mf, mm)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_HareValidateLayersTooDelayed(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	startWithSyncedState(t, syncer)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	patrol := mocks.NewMocklayerPatrol(ctrl)
	syncer.patrol = patrol
	validator := mocks.NewMocklayerValidator(ctrl)
	syncer.validator = validator

	gLayer := types.GetEffectiveGenesis()
	latestLyr := gLayer.Add(maxHareDelayLayers + 1)

	// cause the latest layer to advance
	b := types.GenLayerBlock(latestLyr, nil)
	err := mm.AddBlockWithTXs(context.TODO(), b)
	require.NoError(t, err)

	patrol.EXPECT().IsHareInCharge(gLayer.Add(1)).Return(true).Times(1)
	patrol.EXPECT().IsHareInCharge(gLayer.Add(2)).Return(true).Times(maxAttemptWithinRun)
	// the 1st layer after genesis, despite having hare started consensus protocol for it,
	// is too much delayed.
	validator.EXPECT().ProcessLayer(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, layerID types.LayerID) error {
			assert.Equal(t, gLayer.Add(1), layerID)
			// cause mesh's processed layer to advance
			mm.ProcessLayerPerHareOutput(ctx, layerID, types.EmptyBlockID)
			return nil
		}).Times(1)

	ticker.advanceToLayer(latestLyr)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow synchronize to finish
	feedLayerResult(gLayer.Add(1), latestLyr.Sub(1), mf, mm)
	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_getLayerFromPeersFailed(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	lyr := types.GetEffectiveGenesis().Add(1)
	ticker.advanceToLayer(lyr.Add(1))
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// this will cause getLayerFromPeers to return an error
	mf.getLayerResultChan(lyr) <- layerfetcher.LayerPromiseResult{
		Layer: lyr,
		Err:   errors.New("something baaahhhhhhd"),
	}
	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_getATXsFailedEpochZero(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(types.NewLayerID(layersPerEpoch * 2))
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	mf.setATXsErrors(0, errors.New("no ATXs. in epoch 0. should be ignored"))
	feedLayerResult(types.GetEffectiveGenesis().Add(1), ticker.GetCurrentLayer(), mf, mm)
	wg.Wait()

	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_getATXsFailedPastEpoch(t *testing.T) {
	lg := logtest.New(t)
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(types.NewLayerID(layersPerEpoch * 4))
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	mf.setATXsErrors(3, errors.New("no ATXs. should fail sync"))
	feedLayerResult(types.GetEffectiveGenesis().Add(1), ticker.GetCurrentLayer(), mf, mm)
	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_getATXsFailedCurrentEpoch(t *testing.T) {
	lg := logtest.New(t)
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	// brings the node to synced state
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))

	gLayer := types.GetEffectiveGenesis()
	lyr := types.NewLayerID(4 * layersPerEpoch)
	mf.setATXsErrors(3, errors.New("no ATXs for current epoch. should fail sync at last layer"))
	for i := gLayer.Add(1); i.Before(lyr); i = i.Add(1) {
		ticker.advanceToLayer(i)
		wg.Add(1)
		go func() {
			assert.True(t, syncer.synchronize(context.TODO()))
			wg.Done()
		}()
		feedLayerResult(i, i, mf, mm)
		wg.Wait()

		assert.True(t, syncer.stateOnTarget())
		assert.True(t, syncer.ListenToGossip())
		assert.True(t, syncer.IsSynced(context.TODO()))
	}

	ticker.advanceToLayer(lyr)
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	feedLayerResult(lyr.Sub(1), lyr.Sub(1), mf, mm)
	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_StaySyncedUponFailure(t *testing.T) {
	lg := logtest.New(t)
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	// brings the node to synced state
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))

	lyr := types.GetEffectiveGenesis().Add(1)
	// now make the second synchronize fail by causing getLayerFromPeers to return an error
	mf.getLayerResultChan(lyr) <- layerfetcher.LayerPromiseResult{
		Layer: lyr,
		Err:   errors.New("something baaahhhhhhd"),
	}
	ticker.advanceToLayer(lyr.Add(1))
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_BecomeNotSyncedUponFailureIfNoGossip(t *testing.T) {
	lg := logtest.New(t)
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	// brings the node to synced state
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))

	gLyr := types.GetEffectiveGenesis()
	// in test the latest layer is always genesis layer if we don't add blocks
	lyr := gLyr.Add(outOfSyncThreshold)
	// now make the second synchronize fail by causing getLayerFromPeers to return an error
	mf.getLayerResultChan(gLyr.Add(1)) <- layerfetcher.LayerPromiseResult{
		Layer: lyr,
		Err:   errors.New("something baaahhhhhhd"),
	}
	ticker.advanceToLayer(lyr.Add(1))
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	wg.Wait()

	assert.False(t, syncer.stateOnTarget())
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

// test the case where the node originally starts from notSynced and eventually becomes synced.
func TestFromNotSyncedToSynced(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	firstLayer := types.GetEffectiveGenesis().Add(1)
	current := firstLayer.Add(5)
	ticker.advanceToLayer(current)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	feedLayerResult(firstLayer, firstLayer, mf, mm)
	// wait till firstLayer's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(firstLayer)
	// the node should remain not synced and not gossiping
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	feedLayerResult(firstLayer.Add(1), current, mf, mm)
	wg.Wait()
	// node should be in gossip sync state
	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, syncer, ticker, mf, mm)
}

// test the case where the node originally starts from notSynced, advances to gossipSync, but falls behind
// to notSynced.
func TestFromGossipSyncToNotSynced(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	firstLayer := types.GetEffectiveGenesis().Add(1)
	current := firstLayer.Add(5)
	ticker.advanceToLayer(current)

	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	feedLayerResult(firstLayer, firstLayer, mf, mm)
	// wait till firstLayer's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(firstLayer)
	// the node should remain not synced and not gossiping
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	feedLayerResult(firstLayer.Add(1), current.Sub(1), mf, mm)
	wg.Wait()
	// node should be in gossip sync state
	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// cause the node to be out of sync again
	newCurrent := current.Add(outOfSyncThreshold)
	ticker.advanceToLayer(newCurrent)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	feedLayerResult(current, current, mf, mm)
	<-mf.getLayerPollChan(current)
	// the node should fall to notSynced
	assert.False(t, syncer.stateOnTarget())
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// allow for sync to complete
	feedLayerResult(current.Add(1), newCurrent.Sub(1), mf, mm)
	wg.Wait()

	// the node should enter gossipSync again
	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, newCurrent, syncer, ticker, mf, mm)
}

// test the case where the node was originally synced, and somehow gets out of sync, but
// eventually become synced again.
func TestFromSyncedToNotSynced(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(t, lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, mm, mf, lg)
	syncedCh := make(chan struct{})
	syncer.RegisterChForSynced(context.TODO(), syncedCh)
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	// the node is synced when it starts syncing when current layer is 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	<-syncedCh
	assert.True(t, syncer.IsSynced(context.TODO()))
	// wait for the first synchronize to finish
	wg.Wait()

	// cause the syncer to get out of synced and then wait again
	firstLayer := types.GetEffectiveGenesis().Add(1)
	current := mm.LatestLayer().Add(outOfSyncThreshold)
	ticker.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	// wait till firstLayer's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(firstLayer)
	// the node should realize it's behind now and set the node to be notSynced
	assert.False(t, syncer.stateOnTarget())
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	feedLayerResult(firstLayer, current.Sub(1), mf, mm)
	wg.Wait()
	// node should be in gossip sync state
	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, syncer, ticker, mf, mm)
}

func waitOutGossipSync(t *testing.T, current types.LayerID, syncer *Syncer, mlt *mockLayerTicker, mf *mockFetcher, mm *mesh.Mesh) {
	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// next layer will be still gossip syncing
	require.Equal(t, types.NewLayerID(2).Uint32(), numGossipSyncLayers)
	require.Equal(t, current.Add(numGossipSyncLayers), syncer.getTargetSyncedLayer())

	var wg sync.WaitGroup
	current = current.Add(1)
	mlt.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	feedLayerResult(syncer.mesh.ProcessedLayer().Add(1), current.Sub(1), mf, mm)
	wg.Wait()
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// done one full layer of gossip sync, now it is synced
	syncedCh := make(chan struct{})
	syncer.RegisterChForSynced(context.TODO(), syncedCh)
	current = current.Add(1)
	mlt.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	feedLayerResult(syncer.mesh.ProcessedLayer().Add(1), current.Sub(1), mf, mm)
	<-syncedCh
	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
	wg.Wait()
}

func TestForceSync(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, newMockLayerTicker(), newMemMesh(t, lg), newMockFetcher(), lg)
	syncedCh := make(chan struct{})
	syncer.RegisterChForSynced(context.TODO(), syncedCh)
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	syncer.ForceSync(context.TODO())
	<-syncedCh
	assert.True(t, syncer.stateOnTarget())
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestMultipleForceSync(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, newMemMesh(t, lg), mf, lg)
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	lyr := types.NewLayerID(1)
	ticker.advanceToLayer(lyr)
	syncer.Start(context.TODO())

	assert.True(t, true, syncer.ForceSync(context.TODO()))
	assert.False(t, false, syncer.ForceSync(context.TODO()))

	// allow synchronize to finish
	syncer.Close()

	// node already shutdown
	assert.False(t, false, syncer.ForceSync(context.TODO()))
}

func TestGetATXsCurrentEpoch(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	mf := newMockFetcher()
	ticker := newMockLayerTicker()
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, newMemMesh(t, lg), mf, lg)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	require.Equal(t, 3, layersPerEpoch)
	mf.setATXsErrors(0, errors.New("no ATXs for epoch 0, expected for epoch 0"))
	mf.setATXsErrors(1, errors.New("no ATXs for epoch 1, error out"))

	// epoch 0
	ticker.advanceToLayer(types.NewLayerID(2))
	// epoch 0, ATXs not requested at any layer
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(0)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(1)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(2)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))

	// epoch 1. expect error at last layer
	ticker.advanceToLayer(types.NewLayerID(5))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(3)))
	assert.Equal(t, uint32(1), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(4)))
	assert.Equal(t, uint32(2), atomic.LoadUint32(&mf.atxsCalls))
	assert.Error(t, syncer.getATXs(context.TODO(), types.NewLayerID(5)))
	assert.Equal(t, uint32(3), atomic.LoadUint32(&mf.atxsCalls))

	// epoch 2
	ticker.advanceToLayer(types.NewLayerID(8))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(6)))
	assert.Equal(t, uint32(4), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(7)))
	assert.Equal(t, uint32(5), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(8)))
	assert.Equal(t, uint32(6), atomic.LoadUint32(&mf.atxsCalls))
}

func TestGetATXsOldAndCurrentEpoch(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	mf := newMockFetcher()
	ticker := newMockLayerTicker()
	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, newMemMesh(t, lg), mf, lg)
	syncer.Start(context.TODO())
	t.Cleanup(func() {
		syncer.Close()
	})

	require.Equal(t, 3, layersPerEpoch)
	mf.setATXsErrors(0, errors.New("no ATXs for epoch 0, expected for epoch 0"))
	mf.setATXsErrors(1, errors.New("no ATXs for epoch 1"))

	ticker.advanceToLayer(types.NewLayerID(8)) // epoch 2
	// epoch 0, ATXs not requested at any layer
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(0)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(1)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(2)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))

	// epoch 1, has error but not requested at layer 3/4
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(3)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(4)))
	assert.Equal(t, uint32(0), atomic.LoadUint32(&mf.atxsCalls))
	// will be requested at layer 5
	assert.Error(t, syncer.getATXs(context.TODO(), types.NewLayerID(5)))
	assert.Equal(t, uint32(1), atomic.LoadUint32(&mf.atxsCalls))

	// epoch 2 is the current epoch. ATXs will be requested at every layer
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(6)))
	assert.Equal(t, uint32(2), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(7)))
	assert.Equal(t, uint32(3), atomic.LoadUint32(&mf.atxsCalls))
	assert.NoError(t, syncer.getATXs(context.TODO(), types.NewLayerID(8)))
	assert.Equal(t, uint32(4), atomic.LoadUint32(&mf.atxsCalls))
}

func TestSyncMissingLayer(t *testing.T) {
	lg := logtest.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ticker := newMockLayerTicker()
	fetcher := mocks.NewMocklayerFetcher(ctrl)
	fetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()

	genesis := types.GetEffectiveGenesis()
	last := genesis.Add(4)
	failed := last.Sub(1)

	memdb := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxStore := database.NewMemDatabase()
	goldenATXID := types.ATXID(types.HexToHash32("77777"))
	atxdb := activation.NewDB(atxStore, nil,
		activation.NewIdentityStore(database.NewMemDatabase()),
		layersPerEpoch, goldenATXID, nil, lg.WithName("atxDB"))
	svmstate := mmocks.NewMockstate(gomock.NewController(t))
	svmstate.EXPECT().GetStateRoot().AnyTimes()

	tortoise := mmocks.NewMocktortoise(ctrl)

	m := mesh.NewMesh(memdb, atxdb, tortoise, nil, svmstate, lg.WithName("mesh"))

	syncer := newSyncerWithoutSyncTimer(context.TODO(), t, conf, ticker, m, fetcher, lg.WithName("syncer"))

	ticker.advanceToLayer(last)
	syncer.Start(context.TODO())

	block := types.Block{}
	block.LayerIndex = failed
	block.TxIDs = []types.TransactionID{{1, 1, 1}}
	require.NoError(t, m.AddBlock(&block))
	require.NoError(t, m.SaveContextualValidity(block.ID(), last, true))

	rst := make(chan layerfetcher.LayerPromiseResult)
	close(rst)
	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		fetcher.EXPECT().PollLayerContent(gomock.Any(), lid).Return(rst)
		m.SetZeroBlockLayer(lid)

		tortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
			Return(lid.Sub(1), lid, false)
	}

	tortoise.EXPECT().HandleIncomingLayer(gomock.Any(), gomock.Any()).
		Return(last.Sub(1), last.Sub(1), false).Times(maxAttemptWithinRun - 1)
	fetcher.EXPECT().PollLayerContent(gomock.Any(), failed).Return(rst).Times(maxAttemptWithinRun - 1)

	require.True(t, syncer.synchronize(context.TODO()))
	require.Equal(t, failed, m.MissingLayer())
	require.Equal(t, m.MissingLayer(), m.ProcessedLayer())
}
