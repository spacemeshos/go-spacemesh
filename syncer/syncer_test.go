package syncer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	return &mockLayerTicker{current: unsafe.Pointer(&types.LayerID{})}
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

func (mf *mockFetcher) feedLayerResult(from, to types.LayerID) {
	for i := from; !i.After(to); i = i.Add(1) {
		err := layerfetcher.ErrZeroLayer
		if i == types.GetEffectiveGenesis() {
			err = nil
		}
		mf.getLayerResultChan(i) <- layerfetcher.LayerPromiseResult{
			Layer: i,
			Err:   err,
		}
	}
}

func (mf *mockFetcher) setATXsErrors(epoch types.EpochID, err error) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.atxsError[epoch] = err
}

type mockValidator struct{}

func (mv *mockValidator) LatestComplete() types.LayerID { return types.LayerID{} }
func (mv *mockValidator) Persist(context.Context) error { return nil }
func (mv *mockValidator) HandleIncomingLayer(_ context.Context, layerID types.LayerID) (types.LayerID, types.LayerID, bool) {
	return layerID, layerID.Sub(1), false
}

func (mv *mockValidator) HandleLateBlocks(_ context.Context, blocks []*types.Block) (types.LayerID, types.LayerID) {
	return blocks[0].Layer(), blocks[0].Layer().Sub(1)
}

func newMemMesh(lg log.Log) *mesh.Mesh {
	memdb := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxStore := database.NewMemDatabase()
	goldenATXID := types.ATXID(types.HexToHash32("77777"))
	atxdb := activation.NewDB(atxStore, activation.NewIdentityStore(database.NewMemDatabase()), memdb, layersPerEpoch, goldenATXID, nil, lg.WithName("atxDB"))
	return mesh.NewMesh(memdb, atxdb, mesh.Config{}, &mockValidator{}, nil, nil, lg.WithName("mesh"))
}

var conf = Configuration{
	SyncInterval: time.Second * 60 * 60 * 24, // long enough that it doesn't kick in during testing
	AlwaysListen: false,
}

func newSyncerWithoutSyncTimer(ctx context.Context, conf Configuration, ticker layerTicker, mesh *mesh.Mesh, fetcher layerFetcher, logger log.Log) *Syncer {
	syncer := NewSyncer(ctx, conf, ticker, mesh, fetcher, logger)
	syncer.syncTimer.Stop()
	return syncer
}

func TestStartAndShutdown(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	ctx, cancel := context.WithCancel(context.TODO())
	conf.SyncInterval = time.Millisecond * 5
	syncer := NewSyncer(ctx, conf, ticker, newMemMesh(lg), newMockFetcher(), lg)
	syncedCh := make(chan struct{})
	syncer.RegisterChForSynced(context.TODO(), syncedCh)

	assert.False(t, syncer.IsSynced(context.TODO()))
	assert.False(t, syncer.ListenToGossip())

	// the node is synced when current layer is 0
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
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, newMemMesh(lg), mf, lg)
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
	mf.feedLayerResult(types.NewLayerID(1), current.Sub(1))
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
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(mm.LatestLayer())
	syncer.Start(context.TODO())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow synchronize to finish
	mf.feedLayerResult(types.NewLayerID(0), mm.LatestLayer())
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Close()
}

func TestSynchronize_getLayerFromPeersFailed(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)
	lyr := types.GetEffectiveGenesis()
	ticker.advanceToLayer(lyr.Add(1))
	syncer.Start(context.TODO())

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

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Close()
}

func TestSynchronize_getATXsFailedEpochZero(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(types.NewLayerID(numGossipSyncLayers * 2))
	syncer.Start(context.TODO())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	mf.setATXsErrors(0, errors.New("no ATXs. in epoch 0. should be ignored"))
	mf.feedLayerResult(types.NewLayerID(0), ticker.GetCurrentLayer())
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Close()
}

func TestSynchronize_getATXsFailedPastEpoch(t *testing.T) {
	lg := logtest.New(t)
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(types.NewLayerID(layersPerEpoch * 2))
	syncer.Start(context.TODO())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	mf.setATXsErrors(1, errors.New("no ATXs. should fail sync"))
	mf.feedLayerResult(types.NewLayerID(0), ticker.GetCurrentLayer())
	wg.Wait()

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Close()
}

func TestSynchronize_getATXsFailedCurrentEpoch(t *testing.T) {
	lg := logtest.New(t)
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)
	syncer.Start(context.TODO())

	// brings the node to synced state
	ticker.advanceToLayer(types.NewLayerID(1))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(types.NewLayerID(0), ticker.GetCurrentLayer())
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))

	mf.setATXsErrors(1, errors.New("no ATXs for current epoch. should fail sync at last layer"))
	mf.feedLayerResult(types.NewLayerID(1), types.NewLayerID(2*layersPerEpoch-1))
	for i := types.NewLayerID(layersPerEpoch); i.Before(types.NewLayerID(2 * layersPerEpoch)); i = i.Add(1) {
		ticker.advanceToLayer(i)
		wg.Add(1)
		go func() {
			assert.True(t, syncer.synchronize(context.TODO()))
			wg.Done()
		}()
		wg.Wait()

		assert.True(t, syncer.ListenToGossip())
		assert.True(t, syncer.IsSynced(context.TODO()))
	}

	ticker.advanceToLayer(types.NewLayerID(2 * layersPerEpoch))
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(types.NewLayerID(0), ticker.GetCurrentLayer())
	wg.Wait()

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Close()
}

func TestSynchronize_SyncZeroBlockFailed(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)
	gLayer := types.GetEffectiveGenesis()
	ticker.advanceToLayer(gLayer.Add(1))
	syncer.Start(context.TODO())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	mf.feedLayerResult(types.NewLayerID(0), gLayer.Sub(1))
	// genesis block has data. this LayerPromiseResult will cause SetZeroBlockLayer() to fail
	mf.getLayerResultChan(gLayer) <- layerfetcher.LayerPromiseResult{
		Layer: gLayer,
		Err:   layerfetcher.ErrZeroLayer,
	}
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, ticker.GetCurrentLayer(), syncer, ticker, mf)
	syncer.Close()
}

// test the case where the node originally starts from notSynced and eventually becomes synced
func TestFromNotSyncedToSynced(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	firstLayer := types.GetEffectiveGenesis()
	current := firstLayer.Add(5)
	ticker.advanceToLayer(current)
	syncer.Start(context.TODO())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(firstLayer, firstLayer)
	// wait till layer 1's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(firstLayer)
	// the node should remain not synced and not gossiping
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	mf.feedLayerResult(firstLayer.Add(1), current)
	wg.Wait()
	// node should be in gossip sync state
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, syncer, ticker, mf)
	syncer.Close()
}

// test the case where the node originally starts from notSynced, advances to gossipSync, but falls behind
// to notSynced.
func TestFromGossipSyncToNotSynced(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	firstLayer := types.GetEffectiveGenesis()
	current := firstLayer.Add(5)
	ticker.advanceToLayer(current)

	syncer.Start(context.TODO())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(firstLayer, firstLayer)
	// wait till layer 1's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(firstLayer)
	// the node should remain not synced and not gossiping
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	mf.feedLayerResult(firstLayer.Add(1), current.Sub(1))
	wg.Wait()
	// node should be in gossip sync state
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
	mf.feedLayerResult(current, current)
	<-mf.getLayerPollChan(current)
	// the node should fall to notSynced
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// allow for sync to complete
	mf.feedLayerResult(current.Add(1), newCurrent.Sub(1))
	wg.Wait()

	// the node should enter gossipSync again
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, newCurrent, syncer, ticker, mf)
	syncer.Close()
}

// test the case where the node was originally synced, and somehow gets out of sync, but
// eventually become synced again
func TestFromSyncedToNotSynced(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, mm, mf, lg)
	syncedCh := make(chan struct{})
	syncer.RegisterChForSynced(context.TODO(), syncedCh)
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Start(context.TODO())

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
	firstLayer := types.GetEffectiveGenesis()
	current := mm.LatestLayer().Add(outOfSyncThreshold)
	ticker.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(firstLayer, firstLayer)
	// wait till layer 1's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(firstLayer)
	// the node should realize it's behind now and set the node to be notSynced
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	mf.feedLayerResult(types.NewLayerID(2), current.Sub(1))
	wg.Wait()
	// node should be in gossip sync state
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, syncer, ticker, mf)
	syncer.Close()
}

func waitOutGossipSync(t *testing.T, current types.LayerID, syncer *Syncer, mlt *mockLayerTicker, mf *mockFetcher) {
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// next layer will be still gossip syncing
	assert.Equal(t, types.NewLayerID(2).Uint32(), numGossipSyncLayers)
	assert.Equal(t, current.Add(numGossipSyncLayers), syncer.getTargetSyncedLayer())

	var wg sync.WaitGroup
	current = current.Add(1)
	mlt.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(syncer.mesh.ProcessedLayer().Add(1), current.Sub(1))
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
	mf.feedLayerResult(syncer.mesh.ProcessedLayer().Add(1), current.Sub(1))
	<-syncedCh
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
	wg.Wait()
}

func TestForceSync(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, newMockLayerTicker(), newMemMesh(lg), newMockFetcher(), lg)
	syncedCh := make(chan struct{})
	syncer.RegisterChForSynced(context.TODO(), syncedCh)
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	syncer.Start(context.TODO())

	syncer.ForceSync(context.TODO())
	<-syncedCh
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
	syncer.Close()
}

func TestMultipleForceSync(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, newMemMesh(lg), mf, lg)
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	lyr := types.NewLayerID(1)
	ticker.advanceToLayer(lyr)
	syncer.Start(context.TODO())

	assert.True(t, true, syncer.ForceSync(context.TODO()))
	assert.False(t, false, syncer.ForceSync(context.TODO()))

	// allow synchronize to finish
	mf.feedLayerResult(lyr, lyr)
	syncer.Close()

	// node already shutdown
	assert.False(t, false, syncer.ForceSync(context.TODO()))
}

func TestGetATXsCurrentEpoch(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	mf := newMockFetcher()
	ticker := newMockLayerTicker()
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, newMemMesh(lg), mf, lg)
	syncer.Start(context.TODO())

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
	syncer.Close()
}

func TestGetATXsOldAndCurrentEpoch(t *testing.T) {
	lg := logtest.New(t).WithName("syncer")
	mf := newMockFetcher()
	ticker := newMockLayerTicker()
	syncer := newSyncerWithoutSyncTimer(context.TODO(), conf, ticker, newMemMesh(lg), mf, lg)
	syncer.Start(context.TODO())

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
	syncer.Close()
}
