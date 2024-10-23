package multipeer_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// FIXME: BlockUntilContext is not included in FakeClock interface.
// This will be fixed in a post-0.4.0 clockwork release, but with a breaking change that
// makes FakeClock a struct instead of an interface.
// See: https://github.com/jonboulle/clockwork/pull/71
type fakeClock interface {
	clockwork.FakeClock
	BlockUntilContext(ctx context.Context, n int) error
}

type peerList struct {
	sync.Mutex
	peers []p2p.Peer
}

func (pl *peerList) add(p p2p.Peer) bool {
	pl.Lock()
	defer pl.Unlock()
	if slices.Contains(pl.peers, p) {
		return false
	}
	pl.peers = append(pl.peers, p)
	return true
}

func (pl *peerList) get() []p2p.Peer {
	pl.Lock()
	defer pl.Unlock()
	return slices.Clone(pl.peers)
}

type multiPeerSyncTester struct {
	*testing.T
	ctrl       *gomock.Controller
	syncBase   *MockSyncBase
	syncRunner *MocksyncRunner
	peers      *peers.Peers
	clock      fakeClock
	reconciler *multipeer.MultiPeerReconciler
	cancel     context.CancelFunc
	eg         errgroup.Group
	// EXPECT() calls should not be done concurrently
	// https://github.com/golang/mock/issues/533#issuecomment-821537840
	mtx sync.Mutex
}

func newMultiPeerSyncTester(t *testing.T) *multiPeerSyncTester {
	ctrl := gomock.NewController(t)
	mt := &multiPeerSyncTester{
		T:          t,
		ctrl:       ctrl,
		syncBase:   NewMockSyncBase(ctrl),
		syncRunner: NewMocksyncRunner(ctrl),
		peers:      peers.New(),
		clock:      clockwork.NewFakeClock().(fakeClock),
	}
	mt.reconciler = multipeer.NewMultiPeerReconciler(mt.syncBase, mt.peers, 32, 24,
		multipeer.WithLogger(zaptest.NewLogger(t)),
		multipeer.WithSyncInterval(time.Minute),
		multipeer.WithSyncPeerCount(6),
		multipeer.WithMinSplitSyncPeers(2),
		multipeer.WithMinSplitSyncCount(90),
		multipeer.WithMaxFullDiff(20),
		multipeer.WithMinCompleteFraction(0.9),
		multipeer.WithNoPeersRecheckInterval(10*time.Second),
		multipeer.WithSyncRunner(mt.syncRunner),
		multipeer.WithClock(mt.clock))
	return mt
}

func (mt *multiPeerSyncTester) addPeers(n int) []p2p.Peer {
	r := make([]p2p.Peer, n)
	for i := 0; i < n; i++ {
		p := p2p.Peer(fmt.Sprintf("peer%d", i+1))
		mt.peers.Add(p)
		r[i] = p
	}
	return r
}

func (mt *multiPeerSyncTester) start() context.Context {
	var ctx context.Context
	ctx, mt.cancel = context.WithTimeout(context.Background(), 10*time.Second)
	mt.eg.Go(func() error { return mt.reconciler.Run(ctx) })
	mt.Cleanup(func() {
		mt.cancel()
		if err := mt.eg.Wait(); err != nil {
			require.ErrorIs(mt, err, context.Canceled)
		}
	})
	return ctx
}

func (mt *multiPeerSyncTester) expectProbe(times int, pr rangesync.ProbeResult) *peerList {
	var pl peerList
	mt.syncBase.EXPECT().Probe(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, p p2p.Peer) (rangesync.ProbeResult, error) {
			require.True(mt, pl.add(p), "peer shouldn't be probed twice")
			require.True(mt, mt.peers.Contains(p))
			return pr, nil
		}).Times(times)
	return &pl
}

func (mt *multiPeerSyncTester) expectSingleProbe(
	peer p2p.Peer,
	pr rangesync.ProbeResult,
) {
	mt.syncBase.EXPECT().Probe(gomock.Any(), peer).DoAndReturn(
		func(_ context.Context, p p2p.Peer) (rangesync.ProbeResult, error) {
			return pr, nil
		})
}

func (mt *multiPeerSyncTester) expectFullSync(pl *peerList, times, numFails int) {
	mt.syncRunner.EXPECT().FullSync(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, peers []p2p.Peer) error {
			require.ElementsMatch(mt, pl.get(), peers)
			// delegate to the real fullsync
			return mt.reconciler.FullSync(ctx, peers)
		})
	mt.syncBase.EXPECT().Derive(gomock.Any()).DoAndReturn(func(p p2p.Peer) multipeer.Syncer {
		mt.mtx.Lock()
		defer mt.mtx.Unlock()
		require.Contains(mt, pl.get(), p)
		s := NewMockSyncer(mt.ctrl)
		s.EXPECT().Peer().Return(p).AnyTimes()
		// TODO: do better job at tracking Release() calls
		s.EXPECT().Release().AnyTimes()
		expSync := s.EXPECT().Sync(gomock.Any(), gomock.Nil(), gomock.Nil())
		if numFails != 0 {
			expSync.Return(errors.New("sync failed"))
			numFails--
		}
		return s
	}).Times(times)
}

// satisfy waits until all the expected mocked calls are made.
func (mt *multiPeerSyncTester) satisfy() {
	require.Eventually(mt, func() bool {
		mt.mtx.Lock()
		defer mt.mtx.Unlock()
		return mt.ctrl.Satisfied()
	}, time.Second, time.Millisecond)
}

func TestMultiPeerSync(t *testing.T) {
	const numSyncs = 3

	t.Run("split sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		ctx := mt.start()
		mt.clock.BlockUntilContext(ctx, 1)
		// Advance by sync interval. No peers yet
		mt.clock.Advance(time.Minute)
		mt.clock.BlockUntilContext(ctx, 1)
		// It is safe to do EXPECT() calls while the MultiPeerReconciler is blocked
		mt.addPeers(10)
		// Advance by peer wait time. After that, 6 peers will be selected
		// randomly and probed
		mt.syncBase.EXPECT().Count().Return(50, nil).AnyTimes()
		for i := 0; i < numSyncs; i++ {
			plSplit := mt.expectProbe(6, rangesync.ProbeResult{
				FP:    "foo",
				Count: 100,
				Sim:   0.5, // too low for full sync
			})
			mt.syncRunner.EXPECT().SplitSync(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, peers []p2p.Peer) error {
					require.ElementsMatch(t, plSplit.get(), peers)
					return nil
				})
			mt.syncBase.EXPECT().Wait()
			mt.clock.BlockUntilContext(ctx, 1)
			plFull := mt.expectProbe(6, rangesync.ProbeResult{
				FP:    "foo",
				Count: 100,
				Sim:   1, // after sync
			})
			mt.expectFullSync(plFull, 6, 0)
			mt.syncBase.EXPECT().Wait()
			if i > 0 {
				mt.clock.Advance(time.Minute)
			} else if i < numSyncs-1 {
				mt.clock.Advance(10 * time.Second)
			}
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("full sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count().Return(100, nil).AnyTimes()
		require.False(t, mt.reconciler.Synced())
		var ctx context.Context
		for i := 0; i < numSyncs; i++ {
			pl := mt.expectProbe(6, rangesync.ProbeResult{
				FP:    "foo",
				Count: 100,
				Sim:   0.99, // high enough for full sync
			})
			mt.expectFullSync(pl, 6, 0)
			mt.syncBase.EXPECT().Wait()
			if i == 0 {
				//nolint:fatcontext
				ctx = mt.start()
			} else {
				// first full sync happens immediately
				mt.clock.Advance(time.Minute)
			}
			mt.clock.BlockUntilContext(ctx, 1)
			mt.satisfy()
		}
		require.True(t, mt.reconciler.Synced())
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("full sync, peers with low count ignored", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		addedPeers := mt.addPeers(6)
		mt.syncBase.EXPECT().Count().Return(1000, nil).AnyTimes()
		require.False(t, mt.reconciler.Synced())
		var ctx context.Context
		for i := 0; i < numSyncs; i++ {
			var pl peerList
			for _, p := range addedPeers[:5] {
				mt.expectSingleProbe(p, rangesync.ProbeResult{
					FP:    "foo",
					Count: 1000,
					Sim:   0.99, // high enough for full sync
				})
				pl.add(p)
			}
			mt.expectSingleProbe(addedPeers[5], rangesync.ProbeResult{
				FP:    "foo",
				Count: 800, // count too low, this peer should be ignored
				Sim:   0.9,
			})
			mt.expectFullSync(&pl, 5, 0)
			mt.syncBase.EXPECT().Wait()
			if i == 0 {
				//nolint:fatcontext
				ctx = mt.start()
			} else {
				// first full sync happens immediately
				mt.clock.Advance(time.Minute)
			}
			mt.clock.BlockUntilContext(ctx, 1)
			mt.satisfy()
		}
		require.True(t, mt.reconciler.Synced())
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("full sync due to low peer count", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		mt.addPeers(1)
		mt.syncBase.EXPECT().Count().Return(50, nil).AnyTimes()
		var ctx context.Context
		for i := 0; i < numSyncs; i++ {
			pl := mt.expectProbe(1, rangesync.ProbeResult{
				FP:    "foo",
				Count: 100,
				Sim:   0.5, // too low for full sync, but will have it anyway
			})
			mt.expectFullSync(pl, 1, 0)
			mt.syncBase.EXPECT().Wait()
			if i == 0 {
				//nolint:fatcontext
				ctx = mt.start()
			} else {
				mt.clock.Advance(time.Minute)
			}
			mt.clock.BlockUntilContext(ctx, 1)
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("probe failure", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count().Return(100, nil).AnyTimes()
		mt.syncBase.EXPECT().Probe(gomock.Any(), gomock.Any()).
			Return(rangesync.ProbeResult{}, errors.New("probe failed"))
		pl := mt.expectProbe(5, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
		// just 5 peers for which the probe worked will be checked
		mt.expectFullSync(pl, 5, 0)
		mt.syncBase.EXPECT().Wait().Times(2)
		ctx := mt.start()
		mt.clock.BlockUntilContext(ctx, 1)
	})

	t.Run("failed peers during full sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count().Return(100, nil).AnyTimes()
		var ctx context.Context
		for i := 0; i < numSyncs; i++ {
			pl := mt.expectProbe(6, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
			mt.expectFullSync(pl, 6, 3)
			mt.syncBase.EXPECT().Wait()
			if i == 0 {
				//nolint:fatcontext
				ctx = mt.start()
			} else {
				mt.clock.Advance(time.Minute)
			}
			mt.clock.BlockUntilContext(ctx, 1)
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("failed synced key handling during full sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count().Return(100, nil).AnyTimes()
		var ctx context.Context
		for i := 0; i < numSyncs; i++ {
			pl := mt.expectProbe(6, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
			mt.expectFullSync(pl, 6, 0)
			mt.syncBase.EXPECT().Wait().Return(errors.New("some handlers failed"))
			if i == 0 {
				//nolint:fatcontext
				ctx = mt.start()
			} else {
				mt.clock.Advance(time.Minute)
			}
			mt.clock.BlockUntilContext(ctx, 1)
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("cancellation during sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count().Return(100, nil).AnyTimes()
		mt.expectProbe(6, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
		mt.syncRunner.EXPECT().FullSync(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, peers []p2p.Peer) error {
				mt.cancel()
				return ctx.Err()
			})
		mt.syncBase.EXPECT().Wait().Times(2)
		ctx := mt.start()
		mt.clock.BlockUntilContext(ctx, 1)
		require.ErrorIs(t, mt.eg.Wait(), context.Canceled)
	})
}
