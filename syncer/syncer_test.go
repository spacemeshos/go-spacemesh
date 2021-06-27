package syncer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/layerfetcher"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/log"

	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/mesh"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	layersPerEpoch = 3
)

func init() {
	types.SetLayersPerEpoch(layersPerEpoch)
}

type mockLayerTicker struct {
	now            time.Time
	current        types.LayerID
	layerStartTime time.Time
}

func newMockLayerTicker() *mockLayerTicker {
	return &mockLayerTicker{}
}
func (mlt *mockLayerTicker) advanceToLayer(layerID types.LayerID) {
	atomic.StoreUint64((*uint64)(&mlt.current), uint64(layerID))
}
func (mlt *mockLayerTicker) GetCurrentLayer() types.LayerID {
	return types.LayerID(atomic.LoadUint64((*uint64)(&mlt.current)))
}
func (mlt *mockLayerTicker) LayerToTime(_ types.LayerID) time.Time {
	return mlt.layerStartTime
}

type mockFetcher struct {
	mu     sync.Mutex
	polled map[types.LayerID]chan struct{}
	result map[types.LayerID]chan layerfetcher.LayerPromiseResult
}

func newMockFetcher() *mockFetcher {
	numLayers := layersPerEpoch * 5
	polled := make(map[types.LayerID]chan struct{}, numLayers)
	result := make(map[types.LayerID]chan layerfetcher.LayerPromiseResult, numLayers)
	for i := 0; i <= numLayers; i++ {
		polled[types.LayerID(i)] = make(chan struct{}, 10)
		result[types.LayerID(i)] = make(chan layerfetcher.LayerPromiseResult, 10)
	}
	return &mockFetcher{result: result, polled: polled}
}
func (mf *mockFetcher) PollLayer(_ context.Context, layerID types.LayerID) chan layerfetcher.LayerPromiseResult {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	mf.polled[layerID] <- struct{}{}
	return mf.result[layerID]
}
func (mf *mockFetcher) GetEpochATXs(_ context.Context, _ types.EpochID) error { return nil }
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
	for i := from; i <= to; i++ {
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

type mockValidator struct{}

func (mv *mockValidator) LatestComplete() types.LayerID { return 0 }
func (mv *mockValidator) Persist() error                { return nil }
func (mv *mockValidator) HandleIncomingLayer(layer *types.Layer) (types.LayerID, types.LayerID) {
	return layer.Index(), layer.Index() - 1
}
func (mv *mockValidator) HandleLateBlock(block *types.Block) (types.LayerID, types.LayerID) {
	return block.Layer(), block.Layer() - 1
}

func newMemMesh(lg log.Log) *mesh.Mesh {
	memdb := mesh.NewMemMeshDB(lg.WithName("meshDB"))
	atxStore := database.NewMemDatabase()
	goldenATXID := types.ATXID(types.HexToHash32("77777"))
	atxdb := activation.NewDB(atxStore, activation.NewIdentityStore(database.NewMemDatabase()), memdb, layersPerEpoch, goldenATXID, nil, log.NewDefault("storeAtx").WithName("atxDB"))
	return mesh.NewMesh(memdb, atxdb, mesh.Config{}, &mockValidator{}, nil, nil, lg.WithName("mesh"))
}

var conf = Configuration{
	SyncInterval:    100 * time.Millisecond,
	ValidationDelta: 30 * time.Millisecond,
	AlwaysListen:    false,
}

func TestStartAndShutdown(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	ctx, cancel := context.WithCancel(context.TODO())
	syncer := NewSyncer(ctx, conf, ticker, newMemMesh(lg), newMockFetcher(), lg)

	assert.False(t, syncer.IsSynced(context.TODO()))
	assert.False(t, syncer.ListenToGossip())

	// the node is synced when current layer is 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		syncer.Start(context.TODO())
		wg.Done()
	}()
	<-syncer.AwaitSynced()
	assert.True(t, syncer.IsSynced(context.TODO()))
	assert.True(t, syncer.ListenToGossip())

	cancel()
	assert.True(t, syncer.isClosed())
	assert.False(t, syncer.synchronize(context.TODO()))
	wg.Wait()
}

func TestSynchronize_OnlyOneSynchronize(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	syncer := NewSyncer(context.TODO(), conf, ticker, newMemMesh(lg), mf, lg)
	ticker.advanceToLayer(1)

	var wg sync.WaitGroup
	wg.Add(2)
	first, second := true, true
	atLeastOneStarted := make(chan struct{}, 2)
	go func() {
		atLeastOneStarted <- struct{}{}
		first = syncer.synchronize(context.TODO())
		wg.Done()
	}()
	go func() {
		atLeastOneStarted <- struct{}{}
		second = syncer.synchronize(context.TODO())
		wg.Done()
	}()

	// allow synchronize to finish
	current := ticker.GetCurrentLayer()
	<-atLeastOneStarted
	mf.feedLayerResult(current, current)
	wg.Wait()

	// one of the synchronize call should fail
	assert.False(t, first && second)
	assert.Equal(t, uint64(1), syncer.run)
}

func TestSynchronize_AllGood(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := NewSyncer(context.TODO(), conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(mm.LatestLayer())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow synchronize to finish
	mf.feedLayerResult(0, mm.LatestLayer())
	wg.Wait()

	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_SyncLayerFailed(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := NewSyncer(context.TODO(), conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(mm.LatestLayer())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	// allow synchronize to finish
	mf.getLayerResultChan(1) <- layerfetcher.LayerPromiseResult{
		Layer: 1,
		Err:   errors.New("something baaahhhhhhd"),
	}
	wg.Wait()

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

func TestSynchronize_SyncZeroBlockFailed(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := NewSyncer(context.TODO(), conf, ticker, mm, mf, lg)
	ticker.advanceToLayer(mm.LatestLayer())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.False(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()

	gLayer := types.GetEffectiveGenesis()
	mf.feedLayerResult(0, gLayer-1)
	// genesis block has data. this LayerPromiseResult will cause SetZeroBlockLayer() to fail
	mf.getLayerResultChan(gLayer) <- layerfetcher.LayerPromiseResult{
		Layer: gLayer,
		Err:   layerfetcher.ErrZeroLayer,
	}
	wg.Wait()

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
}

// test the case where the node originally starts from notSynced and eventually becomes synced
func TestFromNotSyncedToSynced(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := NewSyncer(context.TODO(), conf, ticker, mm, mf, lg)

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	current := mm.LatestLayer()
	ticker.advanceToLayer(current)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(1, 1)
	// wait till layer 1's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(1)
	// the node should remain not synced and not gossiping
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))
	mf.feedLayerResult(2, current)
	wg.Wait()
	// node should be in gossip sync state
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, syncer, ticker, mf)
}

// test the case where the node was originally synced, and somehow gets out of sync, but
// eventually become synced again
func TestFromSyncedToNotSynced(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	mf := newMockFetcher()
	mm := newMemMesh(lg)
	syncer := NewSyncer(context.TODO(), conf, ticker, mm, mf, lg)

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// the node is synced when it starts syncing when current layer is 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	<-syncer.AwaitSynced()
	assert.True(t, syncer.IsSynced(context.TODO()))
	// wait for the first synchronize to finish
	wg.Wait()

	// cause the syncer to get out of synced and then wait again
	current := mm.LatestLayer() + outOfSyncThreshold
	ticker.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(1, 1)
	// wait till layer 1's content is requested to check whether sync state has changed
	<-mf.getLayerPollChan(1)
	// the node should realized it's behind now and set the node to be notSynced
	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	mf.feedLayerResult(2, current)
	wg.Wait()
	// node should be in gossip sync state
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	waitOutGossipSync(t, current, syncer, ticker, mf)
}

func waitOutGossipSync(t *testing.T, current types.LayerID, syncer *Syncer, mlt *mockLayerTicker, mf *mockFetcher) {
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// next layer will be still gossip syncing
	assert.Equal(t, types.LayerID(2), numGossipSyncLayers)
	assert.Equal(t, current+numGossipSyncLayers, syncer.getTargetSyncedLayer())

	var wg sync.WaitGroup
	current++
	mlt.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(syncer.mesh.ProcessedLayer()+1, current)
	wg.Wait()
	assert.True(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	// done one full layer of gossip sync, now it is synced
	current++
	mlt.advanceToLayer(current)
	wg.Add(1)
	go func() {
		assert.True(t, syncer.synchronize(context.TODO()))
		wg.Done()
	}()
	mf.feedLayerResult(syncer.mesh.ProcessedLayer()+1, current)
	<-syncer.AwaitSynced()
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
	wg.Wait()
}

func TestForceSync(t *testing.T) {
	lg := log.NewDefault("syncer")
	syncer := NewSyncer(context.TODO(), conf, newMockLayerTicker(), newMemMesh(lg), newMockFetcher(), lg)

	assert.False(t, syncer.ListenToGossip())
	assert.False(t, syncer.IsSynced(context.TODO()))

	syncer.ForceSync(context.TODO())
	<-syncer.AwaitSynced()
	assert.True(t, syncer.ListenToGossip())
	assert.True(t, syncer.IsSynced(context.TODO()))
}

func TestShouldValidateLayer(t *testing.T) {
	lg := log.NewDefault("syncer")
	ticker := newMockLayerTicker()
	syncer := NewSyncer(context.TODO(), conf, ticker, newMemMesh(lg), newMockFetcher(), lg)
	assert.False(t, syncer.shouldValidateLayer(0))

	ticker.advanceToLayer(3)
	assert.False(t, syncer.shouldValidateLayer(0))
	assert.True(t, syncer.shouldValidateLayer(1))
	assert.True(t, syncer.shouldValidateLayer(2))
	// current layer
	ticker.layerStartTime = time.Now()
	assert.False(t, syncer.shouldValidateLayer(3))
	// but if current layer has elapsed ValidationDelta ms, we will validate
	ticker.layerStartTime = time.Now().Add(-conf.ValidationDelta)
	assert.True(t, syncer.shouldValidateLayer(3))
}
