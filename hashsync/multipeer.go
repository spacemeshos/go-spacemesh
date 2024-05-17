package hashsync

import (
	"context"
	"errors"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

type syncability struct {
	// peers that were probed successfully
	syncable []p2p.Peer
	// peers that have enough items for split sync
	splitSyncable []p2p.Peer
	// Number of peers that are similar enough to this one for full sync
	nearFullCount int
}

type MultiPeerReconcilerOpt func(mpr *MultiPeerReconciler)

func WithSyncPeerCount(count int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.syncPeerCount = count
	}
}

func WithMinFullSyncCount(count int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minSplitSyncCount = count
	}
}

func WithMaxFullDiff(diff int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.maxFullDiff = diff
	}
}

func WithSyncInterval(d time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.syncInterval = d
	}
}

func WithNoPeersRecheckInterval(d time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.noPeersRecheckInterval = d
	}
}

func WithMinSplitSyncPeers(n int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minSplitSyncPeers = n
	}
}

func WithMinCompleteFraction(f float64) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minCompleteFraction = f
	}
}

func WithSplitSyncGracePeriod(t time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.splitSyncGracePeriod = t
	}
}

func withClock(clock clockwork.Clock) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.clock = clock
	}
}

func withSyncRunner(runner syncRunner) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.runner = runner
	}
}

type runner struct {
	mpr *MultiPeerReconciler
}

var _ syncRunner = &runner{}

func (r *runner) splitSync(ctx context.Context, syncPeers []p2p.Peer) error {
	s := newSplitSync(
		r.mpr.logger, r.mpr.syncBase, r.mpr.peers, syncPeers,
		r.mpr.splitSyncGracePeriod, r.mpr.clock)
	return s.sync(ctx)
}

func (r *runner) fullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	return r.mpr.fullSync(ctx, syncPeers)
}

type MultiPeerReconciler struct {
	logger                 *zap.Logger
	syncBase               syncBase
	peers                  *peers.Peers
	syncPeerCount          int
	minSplitSyncPeers      int
	minSplitSyncCount      int
	maxFullDiff            int
	minCompleteFraction    float64
	splitSyncGracePeriod   time.Duration
	syncInterval           time.Duration
	noPeersRecheckInterval time.Duration
	clock                  clockwork.Clock
	runner                 syncRunner
}

func NewMultiPeerReconciler(
	logger *zap.Logger,
	syncBase syncBase,
	peers *peers.Peers,
	opts ...MultiPeerReconcilerOpt,
) *MultiPeerReconciler {
	mpr := &MultiPeerReconciler{
		logger:                 logger,
		syncBase:               syncBase,
		peers:                  peers,
		syncPeerCount:          20,
		minSplitSyncPeers:      2,
		minSplitSyncCount:      1000,
		maxFullDiff:            10000,
		syncInterval:           5 * time.Minute,
		minCompleteFraction:    0.5,
		splitSyncGracePeriod:   time.Minute,
		noPeersRecheckInterval: 30 * time.Second,
		clock:                  clockwork.NewRealClock(),
	}
	for _, opt := range opts {
		opt(mpr)
	}
	if mpr.runner == nil {
		mpr.runner = &runner{mpr: mpr}
	}
	return mpr
}

func (mpr *MultiPeerReconciler) probePeers(ctx context.Context, syncPeers []p2p.Peer) (syncability, error) {
	var s syncability
	for _, p := range syncPeers {
		pr, err := mpr.syncBase.probe(ctx, p)
		if err != nil {
			log.Warning("error probing the peer", zap.Any("peer", p), zap.Error(err))
			if errors.Is(err, context.Canceled) {
				return s, err
			}
			continue
		}
		s.syncable = append(s.syncable, p)
		if pr.Count > mpr.minSplitSyncCount {
			s.splitSyncable = append(s.splitSyncable, p)
		}
		if (1-pr.Sim)*float64(mpr.syncBase.count()) < float64(mpr.maxFullDiff) {
			s.nearFullCount++
		}
	}
	return s, nil
}

func (mpr *MultiPeerReconciler) needSplitSync(s syncability) bool {
	if float64(s.nearFullCount) >= float64(mpr.syncBase.count())*mpr.minCompleteFraction {
		// enough peers are close to this one according to minhash score, can do
		// full sync
		return false
	}

	if len(s.splitSyncable) < mpr.minSplitSyncPeers {
		// would be nice to do split sync, but not enough peers for that
		return false
	}

	return true
}

func (mpr *MultiPeerReconciler) fullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	var eg errgroup.Group
	for _, p := range syncPeers {
		syncer := mpr.syncBase.derive(p)
		eg.Go(func() error {
			err := syncer.sync(ctx, nil, nil)
			switch {
			case err == nil:
			case errors.Is(err, context.Canceled):
				return err
			default:
				// failing to sync against a particular peer is not considered
				// a fatal sync failure, so we just log the error
				mpr.logger.Error("error syncing peer", zap.Stringer("peer", p), zap.Error(err))
			}
			return nil
		})
	}
	return eg.Wait()
}

func (mpr *MultiPeerReconciler) syncOnce(ctx context.Context) error {
	var (
		s   syncability
		err error
	)
	for {
		syncPeers := mpr.peers.SelectBest(mpr.syncPeerCount)
		if len(syncPeers) != 0 {
			// probePeers doesn't return transient errors, sync must stop if it failed
			s, err = mpr.probePeers(ctx, syncPeers)
			if err != nil {
				return err
			}
			if len(s.syncable) != 0 {
				break
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-mpr.clock.After(mpr.noPeersRecheckInterval):
		}
	}

	if mpr.needSplitSync(s) {
		return mpr.runner.splitSync(ctx, s.splitSyncable)
	} else {
		return mpr.runner.fullSync(ctx, s.splitSyncable)
	}
}

func (mpr *MultiPeerReconciler) Run(ctx context.Context) error {
	// The point of using split sync, which syncs different key ranges against
	// different peers, vs full sync which syncs the full key range against different
	// peers, is:
	// 1. Avoid getting too many range splits and thus network transfer overhead
	// 2. Avoid fetching same keys from multiple peers

	// States:
	// A. Wait. Pause for sync interval
	//    Timeout => A
	// B. No peers -> do nothing.
	//    Got any peers => C
	// C. Low on peers. Wait for more to appear
	//    Lost all peers => B
	//    Got enough peers => D
	//    Timeout => D
	// D. Probe the peers. Use successfully probed ones in states E/F
	//      Drop failed peers from the peer set while polling.
	//    All probes failed => B
	//    N of peers < minSplitSyncPeers => E
	//    All are low on count (minSplitSyncCount) => F
	//    Enough peers (minCompleteFraction) with diffSize <= maxFullDiff => E
	//      diffSize = (1-sim)*localItemCount
	//    Otherwise => F
	// E. Full sync. Run full syncs against each peer
	//    All syncs completed (success / fail) => A
	// F. Bounded sync. Subdivide the range by peers and start syncs.
	//      Use peers with > minSplitSyncCount
	//      Wait for all the syncs to complete/fail
	//    All syncs completed (success / fail) => A
	ctx, cancel := context.WithCancel(ctx)
	var err error
LOOP:
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break LOOP
		case <-mpr.clock.After(mpr.syncInterval):
		}

		if err = mpr.syncOnce(ctx); err != nil {
			break
		}
	}
	cancel()
	return errors.Join(err, mpr.syncBase.wait())
}
