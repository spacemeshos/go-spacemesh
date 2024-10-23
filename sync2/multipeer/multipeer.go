package multipeer

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
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

// MultiPeerReconcilerOpt specifies an option for a MultiPeerReconciler.
type MultiPeerReconcilerOpt func(mpr *MultiPeerReconciler)

// WithSyncPeerCount sets the number of peers to pick for synchronization.
// Synchronization will still happen if fewer peers are available.
func WithSyncPeerCount(count int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.syncPeerCount = count
	}
}

// WithMinSplitSyncCount sets the minimum number of items that a peer must have to be
// eligible for split sync (subrange-per-peer).
func WithMinSplitSyncCount(count int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minSplitSyncCount = count
	}
}

// WithMaxFullDiff specifies the maximum approximate size of symmetric difference between
// the local set and the remote one for the sets to be considered "mostly in sync", so
// that full sync is preferred to split sync.
func WithMaxFullDiff(diff int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.maxFullDiff = diff
	}
}

// WithMaxSyncDiff specifies the maximum number of items that a peer can have less than the
// local set for it to be considered for synchronization.
func WithMaxSyncDiff(diff int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.maxSyncDiff = diff
	}
}

// WithSyncInterval specifies the interval between syncs.
func WithSyncInterval(d time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.syncInterval = d
	}
}

// WithRetryInterval specifies the interval between retries after a failed sync.
func WithRetryInterval(d time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.retryInterval = d
	}
}

// WithNoPeersRecheckInterval specifies the interval between rechecking for peers after no
// synchronization peers were found.
func WithNoPeersRecheckInterval(d time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.noPeersRecheckInterval = d
	}
}

// WithMinSplitSyncPeers specifies the minimum number of peers for the split
// sync to happen.
func WithMinSplitSyncPeers(n int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minSplitSyncPeers = n
	}
}

// WithMinCompleteFraction specifies the minimum fraction (0..1) of "mostly synced" peers
// starting with which full sync is used instead of split sync.
func WithMinCompleteFraction(f float64) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minCompleteFraction = f
	}
}

// WithSplitSyncGracePeriod specifies grace period for split sync peers.
// If a peer doesn't complete syncing its range within the specified duration during split
// sync, its range is assigned additionally to another quicker peer. The sync against the
// "slow" peer is NOT stopped immediately after that.
func WithSplitSyncGracePeriod(t time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.splitSyncGracePeriod = t
	}
}

// WithLogger specifies the logger for the MultiPeerReconciler.
func WithLogger(logger *zap.Logger) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.logger = logger
	}
}

// WithMinFullSyncednessCount sets the minimum number of full syncs that must
// have happened within the fullSyncednessPeriod for the node to be considered
// fully synced.
func WithMinFullSyncednessCount(count int) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.minFullSyncednessCount = count
	}
}

// WithFullSyncednessPeriod sets the duration within which the minimum number
// of full syncs must have happened for the node to be considered fully synced.
func WithFullSyncednessPeriod(d time.Duration) MultiPeerReconcilerOpt {
	return func(mpr *MultiPeerReconciler) {
		mpr.fullSyncednessPeriod = d
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

func (r *runner) SplitSync(ctx context.Context, syncPeers []p2p.Peer) error {
	s := newSplitSync(
		r.mpr.logger, r.mpr.syncBase, r.mpr.peers, syncPeers,
		r.mpr.splitSyncGracePeriod, r.mpr.clock, r.mpr.keyLen, r.mpr.maxDepth)
	return s.Sync(ctx)
}

func (r *runner) FullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	return r.mpr.fullSync(ctx, syncPeers)
}

// MultiPeerReconciler reconcilies the local set against multiple remote sets.
type MultiPeerReconciler struct {
	logger                 *zap.Logger
	syncBase               SyncBase
	peers                  *peers.Peers
	syncPeerCount          int
	minSplitSyncPeers      int
	minSplitSyncCount      int
	maxFullDiff            int
	maxSyncDiff            int
	minCompleteFraction    float64
	splitSyncGracePeriod   time.Duration
	syncInterval           time.Duration
	retryInterval          time.Duration
	noPeersRecheckInterval time.Duration
	clock                  clockwork.Clock
	keyLen                 int
	maxDepth               int
	runner                 syncRunner
	minFullSyncednessCount int
	fullSyncednessPeriod   time.Duration
	sl                     *syncList
}

// NewMultiPeerReconciler creates a new MultiPeerReconciler.
func NewMultiPeerReconciler(
	syncBase SyncBase,
	peers *peers.Peers,
	keyLen, maxDepth int,
	opts ...MultiPeerReconcilerOpt,
) *MultiPeerReconciler {
	mpr := &MultiPeerReconciler{
		logger:                 zap.NewNop(),
		syncBase:               syncBase,
		peers:                  peers,
		syncPeerCount:          20,
		minSplitSyncPeers:      2,
		minSplitSyncCount:      1000,
		maxFullDiff:            10000,
		maxSyncDiff:            100,
		syncInterval:           5 * time.Minute,
		retryInterval:          1 * time.Minute,
		minCompleteFraction:    0.5,
		splitSyncGracePeriod:   time.Minute,
		noPeersRecheckInterval: 30 * time.Second,
		clock:                  clockwork.NewRealClock(),
		keyLen:                 keyLen,
		maxDepth:               maxDepth,
		minFullSyncednessCount: 1, // TODO: use at least 3 and make it configurable
		fullSyncednessPeriod:   15 * time.Minute,
	}
	for _, opt := range opts {
		opt(mpr)
	}
	if mpr.runner == nil {
		mpr.runner = &runner{mpr: mpr}
	}
	mpr.sl = newSyncList(mpr.clock, mpr.minFullSyncednessCount, mpr.fullSyncednessPeriod)
	return mpr
}

func (mpr *MultiPeerReconciler) probePeers(ctx context.Context, syncPeers []p2p.Peer) (syncability, error) {
	var s syncability
	s.syncable = nil
	s.splitSyncable = nil
	s.nearFullCount = 0
	for _, p := range syncPeers {
		mpr.logger.Debug("probe peer", zap.Stringer("peer", p))
		pr, err := mpr.syncBase.Probe(ctx, p)
		if err != nil {
			mpr.logger.Warn("error probing the peer", zap.Any("peer", p), zap.Error(err))
			if errors.Is(err, context.Canceled) {
				return s, err
			}
			continue
		}

		c, err := mpr.syncBase.Count()
		if err != nil {
			return s, err
		}

		// We do not consider peers with substantially fewer items than the local
		// set for active sync. It's these peers' responsibility to request sync
		// against this node.
		if pr.Count+mpr.maxSyncDiff < c {
			mpr.logger.Debug("skipping peer with low item count",
				zap.Int("peerCount", pr.Count),
				zap.Int("localCount", c))
			continue
		}

		s.syncable = append(s.syncable, p)
		if pr.Count > mpr.minSplitSyncCount {
			mpr.logger.Debug("splitSyncable peer",
				zap.Stringer("peer", p),
				zap.Int("count", pr.Count))
			s.splitSyncable = append(s.splitSyncable, p)
		} else {
			mpr.logger.Debug("NOT splitSyncable peer",
				zap.Stringer("peer", p),
				zap.Int("count", pr.Count))
		}

		mDiff := float64(mpr.maxFullDiff)
		if math.Abs(float64(pr.Count-c)) < mDiff && (1-pr.Sim)*float64(c) < mDiff {
			mpr.logger.Debug("nearFull peer",
				zap.Stringer("peer", p),
				zap.Float64("sim", pr.Sim),
				zap.Int("localCount", c))
			s.nearFullCount++
		} else {
			mpr.logger.Debug("nearFull peer",
				zap.Stringer("peer", p),
				zap.Float64("sim", pr.Sim),
				zap.Int("localCount", c))
		}
	}
	return s, nil
}

func (mpr *MultiPeerReconciler) needSplitSync(s syncability) bool {
	if float64(s.nearFullCount) >= float64(len(s.syncable))*mpr.minCompleteFraction {
		// enough peers are close to this one according to minhash score, can do
		// full sync
		mpr.logger.Debug("enough peers are close to this one, doing full sync",
			zap.Int("nearFullCount", s.nearFullCount),
			zap.Int("peerCount", len(s.syncable)),
			zap.Float64("minCompleteFraction", mpr.minCompleteFraction))
		return false
	}

	if len(s.splitSyncable) < mpr.minSplitSyncPeers {
		// would be nice to do split sync, but not enough peers for that
		mpr.logger.Debug("not enough peers for split sync",
			zap.Int("splitSyncableCount", len(s.splitSyncable)),
			zap.Int("minSplitSyncPeers", mpr.minSplitSyncPeers))
		return false
	}

	mpr.logger.Debug("can do split sync")
	return true
}

func (mpr *MultiPeerReconciler) fullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	var eg errgroup.Group
	for _, p := range syncPeers {
		syncer := mpr.syncBase.Derive(p)
		eg.Go(func() error {
			defer syncer.Release()
			err := syncer.Sync(ctx, nil, nil)
			switch {
			case err == nil:
				mpr.sl.NoteSync()
			case errors.Is(err, context.Canceled):
				return err
			default:
				// failing to sync against a particular peer is not considered
				// a fatal sync failure, so we just log the error
				mpr.logger.Error("error syncing peer", zap.Stringer("peer", p), zap.Error(err))
			}
			return syncer.Release()
		})
	}
	return eg.Wait()
}

func (mpr *MultiPeerReconciler) syncOnce(ctx context.Context, lastWasSplit bool) (full bool, err error) {
	var s syncability
	for {
		syncPeers := mpr.peers.SelectBest(mpr.syncPeerCount)
		mpr.logger.Debug("selected best peers for sync",
			zap.Int("syncPeerCount", mpr.syncPeerCount),
			zap.Int("totalPeers", mpr.peers.Total()),
			zap.Int("numSelected", len(syncPeers)))
		if len(syncPeers) != 0 {
			// probePeers doesn't return transient errors, sync must stop if it failed
			mpr.logger.Debug("probing peers", zap.Int("count", len(syncPeers)))
			s, err = mpr.probePeers(ctx, syncPeers)
			if err != nil {
				return false, err
			}
			if len(s.syncable) != 0 {
				break
			}
		}

		mpr.logger.Debug("no peers found, waiting", zap.Duration("duration", mpr.noPeersRecheckInterval))
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-mpr.clock.After(mpr.noPeersRecheckInterval):
		}
	}

	full = false
	if !lastWasSplit && mpr.needSplitSync(s) {
		mpr.logger.Debug("doing split sync", zap.Int("peerCount", len(s.splitSyncable)))
		err = mpr.runner.SplitSync(ctx, s.splitSyncable)
		if err != nil {
			mpr.logger.Debug("split sync failed", zap.Error(err))
		} else {
			mpr.logger.Debug("split sync complete")
		}
	} else {
		full = true
		mpr.logger.Debug("doing full sync", zap.Int("peerCount", len(s.syncable)))
		err = mpr.runner.FullSync(ctx, s.syncable)
		if err != nil {
			mpr.logger.Debug("full sync failed", zap.Error(err))
		} else {
			mpr.logger.Debug("full sync complete")
		}
	}

	// handler errors are not fatal
	if handlerErr := mpr.syncBase.Wait(); handlerErr != nil {
		mpr.logger.Error("error handling synced keys", zap.Error(handlerErr))
	}

	return full, err
}

// Run runs the MultiPeerReconciler.
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
	//    Last sync was split sync => E
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
	var (
		err  error
		full bool
	)
	lastWasSplit := false
LOOP:
	for {
		interval := mpr.syncInterval
		full, err = mpr.syncOnce(ctx, lastWasSplit)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				break
			}
			mpr.logger.Error("sync failed", zap.Bool("full", full), zap.Error(err))
			interval = mpr.retryInterval
		} else if !full {
			// Split sync needs to be followed by a full sync.
			// Don't wait to have sync move forward quicker.
			// In most cases, the full sync will be very quick.
			lastWasSplit = true
			mpr.logger.Debug("redo sync after split sync")
			continue
		}
		lastWasSplit = false
		mpr.logger.Debug("pausing sync", zap.Duration("interval", interval))
		select {
		case <-ctx.Done():
			err = ctx.Err()
			break LOOP
		case <-mpr.clock.After(interval):
		}
	}
	cancel()
	if err != nil {
		mpr.syncBase.Wait()
		return err
	}
	return mpr.syncBase.Wait()
}

// Synced returns true if the node is considered synced, that is, the specified
// number of full syncs has happened within the specified duration of time.
func (mpr *MultiPeerReconciler) Synced() bool {
	return mpr.sl.Synced()
}
