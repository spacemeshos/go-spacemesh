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
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh"
	mmocks "github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
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
		SyncInterval:     interval,
		SyncCertDistance: 4,
		HareDelayLayers:  5,
	}
	ts.syncer = NewSyncer(ts.cdb, mt, ts.mBeacon, ts.msh, nil, ts.mLyrPatrol, ts.mCertHdr,
		WithConfig(cfg),
		WithLogger(lg),
		withDataFetcher(ts.mDataFetcher),
		withForkFinder(ts.mForkFinder))
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
	current := types.NewLayerID(10)
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

	require.Equal(t, uint64(1), ts.syncer.run)
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
	require.Equal(t, current.GetEpoch(), ts.syncer.getLastSyncedATXs())
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
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch())
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).Return(errors.New("meh"))

	require.False(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, lyr.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, current.GetEpoch(), ts.syncer.getLastSyncedATXs())
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
	require.EqualValues(t, current.GetEpoch(), ts.syncer.getLastSyncedATXs())
	require.Equal(t, lyr.Sub(1), ts.syncer.getLastSyncedLayer())
}

func TestSynchronize_FailedInitialATXsSync(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	failedEpoch := types.EpochID(4)
	current := types.NewLayerID(layersPerEpoch * uint32(failedEpoch+1))
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
	require.Equal(t, failedEpoch-1, ts.syncer.getLastSyncedATXs())
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
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	current := gLayer.Add(2)
	ts.mTicker.advanceToLayer(current)
	lyr := current.Sub(1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID, _ ...p2p.Peer) error {
			ts.msh.SetZeroBlockLayer(context.Background(), got)
			return nil
		})

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.Background()))
	return current
}

func TestSynchronize_SyncEpochATXAtFirstLayer(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)

	require.Equal(t, lyr.GetEpoch()-1, ts.syncer.getLastSyncedATXs())
	current := lyr.Add(2)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), current.GetEpoch()-1)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), gomock.Any()).Times(2)

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.Equal(t, current.GetEpoch()-1, ts.syncer.getLastSyncedATXs())
}

func TestSynchronize_StaySyncedUponFailure(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	lyr := startWithSyncedState(t, ts)
	current := lyr.Add(1)
	ts.mTicker.advanceToLayer(current)
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
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID, _ ...p2p.Peer) error {
				ts.msh.SetZeroBlockLayer(context.Background(), got)
				return nil
			})
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
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID, _ ...p2p.Peer) error {
			ts.msh.SetZeroBlockLayer(context.Background(), got)
			return nil
		})

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
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID, _ ...p2p.Peer) error {
				ts.msh.SetZeroBlockLayer(context.Background(), got)
				return nil
			})
	}
	require.True(t, ts.syncer.synchronize(context.Background()))
	// the node should enter gossipSync again
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	waitOutGossipSync(t, current, ts)
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
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid).DoAndReturn(
			func(_ context.Context, got types.LayerID, _ ...p2p.Peer) error {
				ts.msh.SetZeroBlockLayer(context.Background(), got)
				return nil
			})
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
	require.Equal(t, types.NewLayerID(2).Uint32(), numGossipSyncLayers)
	require.Equal(t, current.Add(numGossipSyncLayers), ts.syncer.getTargetSyncedLayer())

	lyr := current
	current = current.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID, _ ...p2p.Peer) error {
			ts.msh.SetZeroBlockLayer(context.Background(), got)
			return nil
		})
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.False(t, ts.syncer.IsSynced(context.Background()))

	// done one full layer of gossip sync, now it is synced
	lyr = lyr.Add(1)
	current = current.Add(1)
	ts.mTicker.advanceToLayer(current)
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lyr).DoAndReturn(
		func(_ context.Context, got types.LayerID, _ ...p2p.Peer) error {
			ts.msh.SetZeroBlockLayer(context.Background(), got)
			return nil
		})
	require.True(t, ts.syncer.synchronize(context.Background()))
	require.True(t, ts.syncer.dataSynced())
	require.True(t, ts.syncer.ListenToATXGossip())
	require.True(t, ts.syncer.ListenToGossip())
	require.True(t, ts.syncer.IsSynced(context.Background()))
}

func genTx(t testing.TB) types.Transaction {
	t.Helper()
	amount := uint64(1)
	price := uint64(100)
	nonce := uint64(11)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	raw := wallet.Spend(signer.PrivateKey(), types.GenerateAddress([]byte("1")), amount, nonce)
	tx := types.Transaction{
		RawTx:    types.NewRawTx(raw),
		TxHeader: &types.TxHeader{},
	}
	tx.MaxGas = 100
	tx.MaxSpend = amount
	tx.GasPrice = price
	tx.Nonce = nonce
	tx.Principal = types.GenerateAddress(signer.PublicKey().Bytes())
	return tx
}

func TestSyncMissingLayer(t *testing.T) {
	ts := newTestSyncer(t, never)
	genesis := types.GetEffectiveGenesis()
	failed := genesis.Add(2)
	last := genesis.Add(4)
	ts.mTicker.advanceToLayer(last)

	ts.mDataFetcher.EXPECT().GetEpochATXs(gomock.Any(), gomock.Any()).AnyTimes()
	ts.mDataFetcher.EXPECT().PollMaliciousProofs(gomock.Any())
	ts.mLyrPatrol.EXPECT().IsHareInCharge(gomock.Any()).Return(false).AnyTimes()
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil).AnyTimes()

	tx := genTx(t)
	block := types.NewExistingBlock(types.RandomBlockID(), types.InnerBlock{
		LayerIndex: failed,
		TxIDs:      []types.TransactionID{tx.ID},
	})
	require.NoError(t, blocks.Add(ts.cdb, block))
	require.NoError(t, certificates.SetHareOutput(ts.cdb, failed, block.ID()))
	require.NoError(t, blocks.SetValid(ts.cdb, block.ID()))
	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		if lid != failed {
			require.NoError(t, certificates.SetHareOutput(ts.cdb, lid, types.EmptyBlockID))
		}
	}

	for lid := genesis.Add(1); lid.Before(last); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid)
		ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(nil, nil)
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().Updates().Return(nil)
		if lid.Before(failed) {
			ts.mVm.EXPECT().Apply(gomock.Any(), nil, nil)
			ts.mConState.EXPECT().UpdateCache(gomock.Any(), lid, types.EmptyBlockID, nil, nil)
			ts.mVm.EXPECT().GetStateRoot()
		}
		if lid != failed {
			ts.msh.SetZeroBlockLayer(context.Background(), lid)
		}
	}

	require.True(t, ts.syncer.synchronize(context.Background()))
	require.NoError(t, ts.syncer.processLayers(context.Background()))
	require.Equal(t, last.Sub(1), ts.syncer.getLastSyncedLayer())
	require.Equal(t, failed, ts.msh.MissingLayer())
	require.Equal(t, last.Sub(1), ts.msh.ProcessedLayer())
	require.Equal(t, failed.Sub(1), ts.msh.LatestLayerInState())

	// test that synchronize will sync from missing layer again
	require.NoError(t, transactions.Add(ts.cdb, &tx, time.Now()))
	ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), failed)
	require.True(t, ts.syncer.synchronize(context.Background()))

	for lid := failed.Sub(1); lid.Before(last); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(nil, nil)
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().Updates().Return(nil)
		if lid == failed {
			ts.mVm.EXPECT().Apply(gomock.Any(), gomock.Any(), gomock.Any())
			ts.mConState.EXPECT().UpdateCache(gomock.Any(), lid, block.ID(), nil, nil)
			ts.mVm.EXPECT().GetStateRoot()
		} else if lid.After(failed) {
			ts.mVm.EXPECT().Apply(gomock.Any(), nil, nil)
			ts.mConState.EXPECT().UpdateCache(gomock.Any(), lid, types.EmptyBlockID, nil, nil)
			ts.mVm.EXPECT().GetStateRoot()
		}
	}
	require.NoError(t, ts.syncer.processLayers(context.Background()))
	require.Equal(t, types.LayerID{}, ts.msh.MissingLayer())
	require.Equal(t, last.Sub(1), ts.msh.ProcessedLayer())
	require.Equal(t, last.Sub(1), ts.msh.LatestLayerInState())
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
	ts.mTortoise.EXPECT().Updates().Return(nil)
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
