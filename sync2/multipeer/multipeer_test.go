package multipeer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
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

type multiPeerSyncTester struct {
	*testing.T
	ctrl          *gomock.Controller
	syncBase      *MockSyncBase
	syncRunner    *MocksyncRunner
	peers         *peers.Peers
	clock         fakeClock
	reconciler    *MultiPeerReconciler
	selectedPeers []p2p.Peer
	cancel        context.CancelFunc
	eg            errgroup.Group
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
	mt.reconciler = NewMultiPeerReconciler(mt.syncBase, mt.peers, 32, 24,
		WithLogger(zaptest.NewLogger(t)),
		WithSyncInterval(time.Minute),
		WithSyncPeerCount(6),
		WithMinSplitSyncPeers(2),
		WithMinSplitSyncCount(90),
		WithMaxFullDiff(20),
		WithMinCompleteFraction(0.9),
		WithNoPeersRecheckInterval(10*time.Second),
		withSyncRunner(mt.syncRunner),
		withClock(mt.clock))
	return mt
}

func (mt *multiPeerSyncTester) addPeers(n int) {
	for i := 1; i <= n; i++ {
		mt.peers.Add(p2p.Peer(fmt.Sprintf("peer%d", i)))
	}
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

func (mt *multiPeerSyncTester) expectProbe(times int, pr rangesync.ProbeResult) {
	mt.selectedPeers = nil
	mt.syncBase.EXPECT().Probe(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, p p2p.Peer) (rangesync.ProbeResult, error) {
			require.NotContains(mt, mt.selectedPeers, p, "peer probed twice")
			require.True(mt, mt.peers.Contains(p))
			mt.selectedPeers = append(mt.selectedPeers, p)
			return pr, nil
		}).Times(times)
}

func (mt *multiPeerSyncTester) expectFullSync(times, numFails int) {
	mt.syncRunner.EXPECT().fullSync(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, peers []p2p.Peer) error {
			require.ElementsMatch(mt, mt.selectedPeers, peers)
			// delegate to the real fullsync
			return mt.reconciler.fullSync(ctx, peers)
		})
	mt.syncBase.EXPECT().Derive(gomock.Any()).DoAndReturn(func(p p2p.Peer) Syncer {
		require.Contains(mt, mt.selectedPeers, p)
		s := NewMockSyncer(mt.ctrl)
		s.EXPECT().Peer().Return(p).AnyTimes()
		expSync := s.EXPECT().Sync(gomock.Any(), gomock.Nil(), gomock.Nil())
		if numFails != 0 {
			expSync.Return(errors.New("sync failed"))
			numFails--
		}
		return s
	}).Times(times)
}

// satisfy waits until all the expected mocked calls are made
func (mt *multiPeerSyncTester) satisfy() {
	require.Eventually(mt, mt.ctrl.Satisfied, time.Second, time.Millisecond)
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
		mt.addPeers(10)
		// Advance by peer wait time. After that, 6 peers will be selected
		// randomly and probed
		mt.syncBase.EXPECT().Count(gomock.Any()).Return(50, nil).AnyTimes()
		for i := 0; i < numSyncs; i++ {
			mt.expectProbe(6, rangesync.ProbeResult{
				FP:    "foo",
				Count: 100,
				Sim:   0.5, // too low for full sync
			})
			mt.syncRunner.EXPECT().splitSync(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, peers []p2p.Peer) error {
					require.ElementsMatch(t, mt.selectedPeers, peers)
					return nil
				})
			mt.syncBase.EXPECT().Wait()
			mt.clock.BlockUntilContext(ctx, 1)
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
		ctx := mt.start()
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count(gomock.Any()).Return(100, nil).AnyTimes()
		for i := 0; i < numSyncs; i++ {
			mt.expectProbe(6, rangesync.ProbeResult{
				FP:    "foo",
				Count: 100,
				Sim:   0.99, // high enough for full sync
			})
			mt.expectFullSync(6, 0)
			mt.syncBase.EXPECT().Wait()
			mt.clock.BlockUntilContext(ctx, 1)
			mt.clock.Advance(time.Minute)
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("full sync due to low peer count", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		ctx := mt.start()
		mt.addPeers(1)
		mt.syncBase.EXPECT().Count(gomock.Any()).Return(50, nil).AnyTimes()
		for i := 0; i < numSyncs; i++ {
			mt.expectProbe(1, rangesync.ProbeResult{
				FP:    "foo",
				Count: 100,
				Sim:   0.5, // too low for full sync, but will have it anyway
			})
			mt.expectFullSync(1, 0)
			mt.syncBase.EXPECT().Wait()
			mt.clock.BlockUntilContext(ctx, 1)
			mt.clock.Advance(time.Minute)
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("probe failure", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		ctx := mt.start()
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count(gomock.Any()).Return(100, nil).AnyTimes()
		mt.syncBase.EXPECT().Probe(gomock.Any(), gomock.Any()).
			Return(rangesync.ProbeResult{}, errors.New("probe failed"))
		mt.expectProbe(5, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
		// just 5 peers for which the probe worked will be checked
		mt.expectFullSync(5, 0)
		mt.syncBase.EXPECT().Wait().Times(2)
		mt.clock.BlockUntilContext(ctx, 1)
		mt.clock.Advance(time.Minute)
	})

	t.Run("failed peers during full sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		ctx := mt.start()
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count(gomock.Any()).Return(100, nil).AnyTimes()
		for i := 0; i < numSyncs; i++ {
			mt.expectProbe(6, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
			mt.expectFullSync(6, 3)
			mt.syncBase.EXPECT().Wait()
			mt.clock.BlockUntilContext(ctx, 1)
			mt.clock.Advance(time.Minute)
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("failed synced key handling during full sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		ctx := mt.start()
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count(gomock.Any()).Return(100, nil).AnyTimes()
		for i := 0; i < numSyncs; i++ {
			mt.expectProbe(6, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
			mt.expectFullSync(6, 0)
			mt.syncBase.EXPECT().Wait().Return(errors.New("some handlers failed"))
			mt.clock.BlockUntilContext(ctx, 1)
			mt.clock.Advance(time.Minute)
			mt.satisfy()
		}
		mt.syncBase.EXPECT().Wait()
	})

	t.Run("cancellation during sync", func(t *testing.T) {
		mt := newMultiPeerSyncTester(t)
		ctx := mt.start()
		mt.addPeers(10)
		mt.syncBase.EXPECT().Count(gomock.Any()).Return(100, nil).AnyTimes()
		mt.expectProbe(6, rangesync.ProbeResult{FP: "foo", Count: 100, Sim: 0.99})
		mt.syncRunner.EXPECT().fullSync(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, peers []p2p.Peer) error {
				mt.cancel()
				return ctx.Err()
			})
		mt.syncBase.EXPECT().Wait().Times(2)
		mt.clock.BlockUntilContext(ctx, 1)
		mt.clock.Advance(time.Minute)
		require.ErrorIs(t, mt.eg.Wait(), context.Canceled)
	})
}
