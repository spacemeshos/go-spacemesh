package sync2

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type Config struct {
	MaxSendRange           int           `mapstructure:"max-send-range"`
	SampleSize             int           `mapstructure:"sample-size"`
	Timeout                time.Duration `mapstructure:"timeout"`
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
}

func DefaultConfig() Config {
	return Config{
		MaxSendRange:           rangesync.DefaultMaxSendRange,
		SampleSize:             rangesync.DefaultSampleSize,
		Timeout:                10 * time.Second,
		SyncPeerCount:          20,
		MinSplitSyncPeers:      2,
		MinSplitSyncCount:      1000,
		MaxFullDiff:            10000,
		SyncInterval:           5 * time.Minute,
		MinCompleteFraction:    0.5,
		SplitSyncGracePeriod:   time.Minute,
		NoPeersRecheckInterval: 30 * time.Second,
	}
}

type P2PHashSync struct {
	logger     *zap.Logger
	cfg        Config
	h          host.Host
	os         rangesync.OrderedSet
	syncBase   multipeer.SyncBase
	reconciler *multipeer.MultiPeerReconciler
	srv        *server.Server
	cancel     context.CancelFunc
	eg         errgroup.Group
	start      sync.Once
	running    atomic.Bool
}

func NewP2PHashSync(
	logger *zap.Logger,
	h host.Host,
	os rangesync.OrderedSet,
	keyLen, maxDepth int,
	proto string,
	peers *peers.Peers,
	handler multipeer.SyncKeyHandler,
	cfg Config,
) *P2PHashSync {
	s := &P2PHashSync{
		logger: logger,
		h:      h,
		os:     os,
		cfg:    cfg,
	}
	s.srv = server.New(h, proto, s.handle,
		server.WithTimeout(cfg.Timeout),
		server.WithLog(logger))
	rangeSyncOpts := []rangesync.RangeSetReconcilerOption{
		rangesync.WithMaxSendRange(cfg.MaxSendRange),
		rangesync.WithSampleSize(cfg.SampleSize),
	}
	if cfg.RecentTimeSpan > 0 {
		rangeSyncOpts = append(rangeSyncOpts, rangesync.WithRecentTimeSpan(cfg.RecentTimeSpan))
	}
	ps := rangesync.NewPairwiseSetSyncer(s.srv, rangeSyncOpts)
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
	return s
}

func (s *P2PHashSync) handle(ctx context.Context, req []byte, stream io.ReadWriter) error {
	if !s.running.Load() {
		return errors.New("sync server not running")
	}
	peer, found := server.ContextPeerID(ctx)
	if !found {
		panic("BUG: no peer ID found in the handler")
	}
	// We derive a dedicated Syncer for the peer being served to pass all the received
	// items through the handler before adding them to the main ItemStore
	syncer := s.syncBase.Derive(peer)
	return syncer.Serve(ctx, req, stream)
}

func (s *P2PHashSync) Set() rangesync.OrderedSet {
	return s.os
}

func (s *P2PHashSync) Start() {
	s.start.Do(func() {
		var ctx context.Context
		ctx, s.cancel = context.WithCancel(context.Background())
		s.eg.Go(func() error {
			s.logger.Info("loading the set")
			// We pre-load the set to avoid waiting for it to load during a
			// sync request
			if err := s.os.EnsureLoaded(ctx); err != nil {
				return err
			}
			if s.cfg.EnableActiveSync {
				s.eg.Go(func() error { return s.reconciler.Run(ctx) })
			}
			return s.srv.Run(ctx)
		})
		s.running.Store(true)
	})
}

func (s *P2PHashSync) Stop() {
	if !s.running.CompareAndSwap(true, false) {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	if err := s.eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("P2PHashSync terminated with an error", zap.Error(err))
	}
}

func (s *P2PHashSync) Synced() bool {
	return s.reconciler.Synced()
}

var errStopped = errors.New("atx syncer stopped")

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
