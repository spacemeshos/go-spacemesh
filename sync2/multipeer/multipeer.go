package multipeer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

const (
	Protocol = "sync/2"
)

type syncability struct {
	// peers that were probed successfully
	syncable []p2p.Peer
	// peers that have enough items for split sync
	splitSyncable []p2p.Peer
	// Number of peers that are similar enough to this one for full sync
	nearFullCount int
}

type runner struct {
	mpr *MultiPeerReconciler
}

var _ syncRunner = &runner{}

func (r *runner) SplitSync(ctx context.Context, syncPeers []p2p.Peer) error {
	s := newSplitSync(
		r.mpr.logger, r.mpr.syncBase, r.mpr.peers, syncPeers,
		r.mpr.cfg.SplitSyncGracePeriod, r.mpr.clock, r.mpr.keyLen, r.mpr.maxDepth)
	return s.Sync(ctx)
}

func (r *runner) FullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	return r.mpr.fullSync(ctx, syncPeers)
}

// MultiPeerReconcilerConfig contains the configuration for a MultiPeerReconciler.
type MultiPeerReconcilerConfig struct {
	// Number of peers to pick for synchronization.
	// Synchronization will still happen if fewer peers are available.
	SyncPeerCount int `mapstructure:"sync-peer-count"`
	// Minimum number of peers for the split sync to happen.
	MinSplitSyncPeers int `mapstructure:"min-split-sync-peers"`
	// Minimum number of items that a peer must have to be eligible for split sync
	// (subrange-per-peer).
	MinSplitSyncCount int `mapstructure:"min-split-sync-count"`
	// Maximum approximate size of symmetric difference between the local set and the
	// remote one for the sets to be considered "mostly in sync", so that full sync is
	// preferred to split sync.
	MaxFullDiff int `mapstructure:"max-full-diff"`
	// Maximum number of items that a peer can have less than the local set for it to
	// be considered for synchronization.
	MaxSyncDiff int `mapstructure:"max-sync-diff"`
	// Minimum fraction (0..1) of "mostly synced" peers starting with which full sync
	// is used instead of split sync.
	MinCompleteFraction float64 `mapstructure:"min-complete-fraction"`
	// Interval between syncs.
	SyncInterval time.Duration `mapstructure:"sync-interval"`
	// Interval spread factor for split sync.
	// The actual interval will be SyncInterval * (1 + (random[0..2]*SplitSyncIntervalSpread-1)).
	SyncIntervalSpread float64 `mapstructure:"sync-interval-spread"`
	// Interval between retries after a failed sync.
	RetryInterval time.Duration `mapstructure:"retry-interval"`
	// Interval between rechecking for peers after no synchronization peers were
	// found.
	NoPeersRecheckInterval time.Duration `mapstructure:"no-peers-recheck-interval"`
	// Grace period for split sync peers.
	// If a peer doesn't complete syncing its range within the specified duration
	// during split sync, its range is assigned additionally to another quicker
	// peer. The sync against the "slow" peer is NOT stopped immediately after that.
	SplitSyncGracePeriod time.Duration `mapstructure:"split-sync-grace-period"`
	// Minimum number of full syncs that must have happened within the
	// fullSyncednessPeriod for the node to be considered fully synced
	MinFullSyncednessCount int `mapstructure:"min-full-syncedness-count"`
	// Duration within which the minimum number of full syncs must have happened for
	// the node to be considered fully synced.
	FullSyncednessPeriod time.Duration `mapstructure:"full-syncedness-count"`
}

// DefaultConfig returns the default configuration for the MultiPeerReconciler.
func DefaultConfig() MultiPeerReconcilerConfig {
	return MultiPeerReconcilerConfig{
		SyncPeerCount:          20,
		MinSplitSyncPeers:      2,
		MinSplitSyncCount:      1000,
		MaxFullDiff:            10000,
		MaxSyncDiff:            100,
		SyncInterval:           5 * time.Minute,
		SyncIntervalSpread:     0.5,
		RetryInterval:          1 * time.Minute,
		NoPeersRecheckInterval: 30 * time.Second,
		SplitSyncGracePeriod:   time.Minute,
		MinCompleteFraction:    0.5,
		MinFullSyncednessCount: 1,
		FullSyncednessPeriod:   15 * time.Minute,
	}
}

// MultiPeerReconciler reconcilies the local set against multiple remote sets.
type MultiPeerReconciler struct {
	logger   *zap.Logger
	cfg      MultiPeerReconcilerConfig
	syncBase SyncBase
	peers    *peers.Peers
	clock    clockwork.Clock
	keyLen   int
	maxDepth int
	runner   syncRunner
	sl       *syncList
}

func newMultiPeerReconciler(
	logger *zap.Logger,
	cfg MultiPeerReconcilerConfig,
	syncBase SyncBase,
	peers *peers.Peers,
	keyLen, maxDepth int,
	syncRunner syncRunner,
	clock clockwork.Clock,
) *MultiPeerReconciler {
	mpr := &MultiPeerReconciler{
		logger:   logger,
		cfg:      cfg,
		syncBase: syncBase,
		peers:    peers,
		clock:    clock,
		keyLen:   keyLen,
		maxDepth: maxDepth,
		runner:   syncRunner,
		sl:       newSyncList(clock, cfg.MinFullSyncednessCount, cfg.FullSyncednessPeriod),
	}
	if mpr.runner == nil {
		mpr.runner = &runner{mpr: mpr}
	}
	return mpr
}

// NewMultiPeerReconciler creates a new MultiPeerReconciler.
func NewMultiPeerReconciler(
	logger *zap.Logger,
	cfg MultiPeerReconcilerConfig,
	syncBase SyncBase,
	peers *peers.Peers,
	keyLen, maxDepth int,
) *MultiPeerReconciler {
	return newMultiPeerReconciler(
		logger, cfg, syncBase, peers, keyLen, maxDepth,
		nil, clockwork.NewRealClock())
}

func (mpr *MultiPeerReconciler) probePeers(ctx context.Context, syncPeers []p2p.Peer) (syncability, error) {
	var s syncability
	s.syncable = nil
	s.splitSyncable = nil
	s.nearFullCount = 0
	type probeResult struct {
		p p2p.Peer
		rangesync.ProbeResult
	}
	probeCh := make(chan probeResult)

	localCount, err := mpr.syncBase.Count()
	if err != nil {
		return syncability{}, err
	}

	var eg errgroup.Group
	for _, p := range syncPeers {
		eg.Go(func() error {
			mpr.logger.Debug("probe peer", zap.Stringer("peer", p))
			pr, err := mpr.syncBase.Probe(ctx, p)
			if err != nil {
				mpr.logger.Warn("error probing the peer", zap.Any("peer", p), zap.Error(err))
				if errors.Is(err, context.Canceled) {
					return err
				}
			} else {
				probeCh <- probeResult{p, pr}
			}
			return nil
		})
	}

	// We need to close probeCh for the loop below to terminate, and we must do that
	// only after all the goroutines above have finished.
	var egWait errgroup.Group
	egWait.Go(func() error {
		defer close(probeCh)
		return eg.Wait()
	})

	for pr := range probeCh {
		// We do not consider peers with substantially fewer items than the local
		// set for active sync. It's these peers' responsibility to request sync
		// against this node.
		if pr.Count+mpr.cfg.MaxSyncDiff < localCount {
			mpr.logger.Debug("skipping peer with low item count",
				zap.Int("peerCount", pr.Count),
				zap.Int("localCount", localCount))
			continue
		}

		s.syncable = append(s.syncable, pr.p)
		if pr.Count > mpr.cfg.MinSplitSyncCount {
			mpr.logger.Debug("splitSyncable peer",
				zap.Stringer("peer", pr.p),
				zap.Int("count", pr.Count))
			s.splitSyncable = append(s.splitSyncable, pr.p)
		} else {
			mpr.logger.Debug("NOT splitSyncable peer",
				zap.Stringer("peer", pr.p),
				zap.Int("count", pr.Count))
		}

		mDiff := float64(mpr.cfg.MaxFullDiff)
		if math.Abs(float64(pr.Count-localCount)) < mDiff && (1-pr.Sim)*float64(localCount) < mDiff {
			mpr.logger.Debug("nearFull peer",
				zap.Stringer("peer", pr.p),
				zap.Float64("sim", pr.Sim),
				zap.Int("localCount", localCount))
			s.nearFullCount++
		} else {
			mpr.logger.Debug("nearFull peer",
				zap.Stringer("peer", pr.p),
				zap.Float64("sim", pr.Sim),
				zap.Int("localCount", localCount))
		}
	}

	return s, egWait.Wait()
}

func (mpr *MultiPeerReconciler) needSplitSync(s syncability) bool {
	mpr.logger.Debug("checking if we need split sync")
	if float64(s.nearFullCount) >= float64(len(s.syncable))*mpr.cfg.MinCompleteFraction {
		// enough peers are close to this one according to minhash score, can do
		// full sync
		mpr.logger.Debug("enough peers are close to this one, doing full sync",
			zap.Int("nearFullCount", s.nearFullCount),
			zap.Int("peerCount", len(s.syncable)),
			zap.Float64("minCompleteFraction", mpr.cfg.MinCompleteFraction))
		return false
	}

	if len(s.splitSyncable) < mpr.cfg.MinSplitSyncPeers {
		// would be nice to do split sync, but not enough peers for that
		mpr.logger.Debug("not enough peers for split sync",
			zap.Int("splitSyncableCount", len(s.splitSyncable)),
			zap.Int("minSplitSyncPeers", mpr.cfg.MinSplitSyncPeers))
		return false
	}

	mpr.logger.Debug("can do split sync")
	return true
}

func (mpr *MultiPeerReconciler) fullSync(ctx context.Context, syncPeers []p2p.Peer) error {
	if len(syncPeers) == 0 {
		return errors.New("no peers to sync against")
	}
	var eg errgroup.Group
	var someSucceeded atomic.Bool
	for _, p := range syncPeers {
		eg.Go(func() error {
			if err := mpr.syncBase.WithPeerSyncer(ctx, p, func(ps PeerSyncer) error {
				err := ps.Sync(ctx, nil, nil)
				switch {
				case err == nil:
					someSucceeded.Store(true)
					mpr.sl.NoteSync()
				case errors.Is(err, context.Canceled):
					return err
				default:
					// failing to sync against a particular peer is not considered
					// a fatal sync failure, so we just log the error
					mpr.logger.Error("error syncing peer",
						zap.Stringer("peer", p),
						zap.Error(err))
				}
				return nil
			}); err != nil {
				return fmt.Errorf("sync %s: %w", p, err)
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	if !someSucceeded.Load() {
		return errors.New("all syncs failed")
	}
	return nil
}

func (mpr *MultiPeerReconciler) syncOnce(ctx context.Context, lastWasSplit bool) (full bool, err error) {
	var s syncability
	for {
		syncPeers := mpr.peers.SelectBestWithProtocols(mpr.cfg.SyncPeerCount, []protocol.ID{Protocol})
		mpr.logger.Debug("selected best peers for sync",
			zap.Int("syncPeerCount", mpr.cfg.SyncPeerCount),
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

		mpr.logger.Debug("no peers found, waiting", zap.Duration("duration", mpr.cfg.NoPeersRecheckInterval))
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-mpr.clock.After(mpr.cfg.NoPeersRecheckInterval):
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
func (mpr *MultiPeerReconciler) Run(ctx context.Context, kickCh chan struct{}) error {
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
	var (
		err  error
		full bool
	)
	lastWasSplit := false
LOOP:
	for {
		interval := time.Duration(
			float64(mpr.cfg.SyncInterval) *
				(1 + mpr.cfg.SyncIntervalSpread*(rand.Float64()*2-1)))
		full, err = mpr.syncOnce(ctx, lastWasSplit)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				break
			}
			mpr.logger.Error("sync failed", zap.Bool("full", full), zap.Error(err))
			interval = mpr.cfg.RetryInterval
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
		case <-kickCh:
		}
	}
	// The loop is only exited upon context cancellation.
	// Thus, syncBase.Wait() is guaranteed not to block indefinitely here.
	mpr.syncBase.Wait()
	return err
}

// Synced returns true if the node is considered synced, that is, the specified
// number of full syncs has happened within the specified duration of time.
func (mpr *MultiPeerReconciler) Synced() bool {
	return mpr.sl.Synced()
}
