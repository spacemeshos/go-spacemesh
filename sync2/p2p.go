package sync2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type Dispatcher = rangesync.Dispatcher

// Config contains the configuration for the P2PHashSync.
type Config struct {
	MaxSendRange           int           `mapstructure:"max-send-range"`
	SampleSize             int           `mapstructure:"sample-size"`
	SyncPeerCount          int           `mapstructure:"sync-peer-count"`
	MinSplitSyncCount      int           `mapstructure:"min-split-sync-count"`
	MaxFullDiff            int           `mapstructure:"max-full-diff"`
	SyncInterval           time.Duration `mapstructure:"sync-interval"`
	NoPeersRecheckInterval time.Duration `mapstructure:"no-peers-recheck-interval"`
	MinSplitSyncPeers      int           `mapstructure:"min-split-sync-peers"`
	MinCompleteFraction    float64       `mapstructure:"min-complete-fraction"`
	SplitSyncGracePeriod   time.Duration `mapstructure:"split-sync-grace-period"`
	RecentTimeSpan         time.Duration `mapstructure:"recent-time-span"`
	EnableActiveSync       bool          `mapstructure:"enable-active-sync"`
	MaxReconcDiff          float64       `mapstructure:"max-reconc-diff"`
	AutoCommitCount        int           `mapstructure:"auto-commit-count"`
	AutoCommitIdle         time.Duration `mapstructure:"auto-commit-idle"`
	TrafficLimit           int           `mapstructure:"traffic-limit"`
	MessageLimit           int           `mapstructure:"message-limit"`
}

// DefaultConfig returns the default configuration for the P2PHashSync.
func DefaultConfig() Config {
	return Config{
		MaxSendRange:           rangesync.DefaultMaxSendRange,
		SampleSize:             rangesync.DefaultSampleSize,
		SyncPeerCount:          20,
		MinSplitSyncPeers:      2,
		MinSplitSyncCount:      1000,
		MaxFullDiff:            10000,
		SyncInterval:           5 * time.Minute,
		MinCompleteFraction:    0.5,
		SplitSyncGracePeriod:   time.Minute,
		NoPeersRecheckInterval: 30 * time.Second,
		MaxReconcDiff:          0.01,
		AutoCommitCount:        10000,
		AutoCommitIdle:         time.Second,
		TrafficLimit:           200_000_000,
		MessageLimit:           20_000_000,
	}
}

// P2PHashSync is handles the synchronization of a local OrderedSet against other peers.
type P2PHashSync struct {
	logger     *zap.Logger
	cfg        Config
	os         multipeer.OrderedSet
	syncBase   multipeer.SyncBase
	reconciler *multipeer.MultiPeerReconciler
	cancel     context.CancelFunc
	eg         errgroup.Group
	start      sync.Once
	running    atomic.Bool
}

// NewP2PHashSync creates a new P2PHashSync.
func NewP2PHashSync(
	logger *zap.Logger,
	d *Dispatcher,
	name string,
	os multipeer.OrderedSet,
	keyLen, maxDepth int,
	peers *peers.Peers,
	handler multipeer.SyncKeyHandler,
	cfg Config,
	requester rangesync.Requester,
) *P2PHashSync {
	s := &P2PHashSync{
		logger: logger,
		os:     os,
		cfg:    cfg,
	}
	rangeSyncOpts := []rangesync.RangeSetReconcilerOption{
		rangesync.WithMaxSendRange(cfg.MaxSendRange),
		rangesync.WithSampleSize(cfg.SampleSize),
		rangesync.WithMaxDiff(cfg.MaxReconcDiff),
		rangesync.WithLogger(logger),
	}
	if cfg.RecentTimeSpan > 0 {
		rangeSyncOpts = append(rangeSyncOpts, rangesync.WithRecentTimeSpan(cfg.RecentTimeSpan))
	}
	// var ps multipeer.PairwiseSyncer
	ps := rangesync.NewPairwiseSetSyncer(requester, name, rangeSyncOpts, []rangesync.ConduitOption{
		rangesync.WithTrafficLimit(cfg.TrafficLimit),
		rangesync.WithMessageLimit(cfg.MessageLimit),
	})
	s.syncBase = multipeer.NewSetSyncBase(ps, s.os, handler)
	s.reconciler = multipeer.NewMultiPeerReconciler(
		s.syncBase, peers, keyLen, maxDepth,
		multipeer.WithLogger(logger),
		multipeer.WithSyncPeerCount(cfg.SyncPeerCount),
		multipeer.WithMinSplitSyncPeers(cfg.MinSplitSyncPeers),
		multipeer.WithMinSplitSyncCount(cfg.MinSplitSyncCount),
		multipeer.WithMaxFullDiff(cfg.MaxFullDiff),
		multipeer.WithSyncInterval(cfg.SyncInterval),
		multipeer.WithMinCompleteFraction(cfg.MinCompleteFraction),
		multipeer.WithSplitSyncGracePeriod(time.Minute),
		multipeer.WithNoPeersRecheckInterval(cfg.NoPeersRecheckInterval))
	d.Register(name, s.serve)
	return s
}

func (s *P2PHashSync) serve(ctx context.Context, stream io.ReadWriter) error {
	peer, found := server.ContextPeerID(ctx)
	if !found {
		panic("BUG: no peer ID found in the handler")
	}
	// We derive a dedicated Syncer for the peer being served to pass all the received
	// items through the handler before adding them to the main ItemStore
	return s.syncBase.Derive(peer).Serve(ctx, stream)
}

// Set returns the OrderedSet that is being synchronized.
func (s *P2PHashSync) Set() rangesync.OrderedSet {
	return s.os
}

// Load loads the OrderedSet from the underlying storage.
func (s *P2PHashSync) Load() error {
	s.logger.Info("loading the set")
	start := time.Now()
	// We pre-load the set to avoid waiting for it to load during a
	// sync request
	if err := s.os.EnsureLoaded(); err != nil {
		return fmt.Errorf("load set: %w", err)
	}
	info, err := s.os.GetRangeInfo(nil, nil)
	if err != nil {
		return fmt.Errorf("get range info: %w", err)
	}
	s.logger.Info("done loading the set",
		zap.Duration("elapsed", time.Since(start)),
		zap.Int("count", info.Count),
		zap.Stringer("fingerprint", info.Fingerprint))
	return nil
}

// Start starts the multi-peer reconciler.
func (s *P2PHashSync) Start() {
	if !s.cfg.EnableActiveSync {
		s.logger.Info("active sync is disabled")
		return
	}
	s.running.Store(true)
	s.start.Do(func() {
		s.eg.Go(func() error {
			defer s.running.Store(false)
			var ctx context.Context
			ctx, s.cancel = context.WithCancel(context.Background())
			return s.reconciler.Run(ctx)
		})
	})
}

// Stop stops the multi-peer reconciler.
func (s *P2PHashSync) Stop() {
	if !s.cfg.EnableActiveSync || !s.running.Load() {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if err := s.eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("P2PHashSync terminated with an error", zap.Error(err))
	}
}

// Synced returns true if the local OrderedSet is in sync with the peers, as determined by
// the multi-peer reconciler.
func (s *P2PHashSync) Synced() bool {
	return s.reconciler.Synced()
}

var errStopped = errors.New("syncer stopped")

// WaitForSync waits until the local OrderedSet is in sync with the peers.
func (s *P2PHashSync) WaitForSync(ctx context.Context) error {
	for !s.Synced() {
		if !s.running.Load() {
			return errStopped
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}
