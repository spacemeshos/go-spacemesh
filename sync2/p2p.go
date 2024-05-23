package hashsync

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
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
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
}

func DefaultConfig() Config {
	return Config{
		MaxSendRange:           hashsync.DefaultMaxSendRange,
		SampleSize:             hashsync.DefaultSampleSize,
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
	h          host.Host
	is         hashsync.ItemStore
	syncBase   hashsync.SyncBase
	reconciler *hashsync.MultiPeerReconciler
	srv        *server.Server
	cancel     context.CancelFunc
	eg         errgroup.Group
	start      sync.Once
	running    atomic.Bool
}

func NewP2PHashSync(
	logger *zap.Logger,
	h host.Host,
	proto string,
	peers *peers.Peers,
	handler hashsync.SyncKeyHandler,
	cfg Config,
) *P2PHashSync {
	s := &P2PHashSync{
		logger: logger,
		h:      h,
		is:     hashsync.NewSyncTreeStore(hashsync.Hash32To12Xor{}),
	}
	s.srv = server.New(h, proto, s.handle,
		server.WithTimeout(cfg.Timeout),
		server.WithLog(logger))
	ps := hashsync.NewPairwiseStoreSyncer(s.srv, []hashsync.RangeSetReconcilerOption{
		hashsync.WithMaxSendRange(cfg.MaxSendRange),
		hashsync.WithSampleSize(cfg.SampleSize),
	})
	s.syncBase = hashsync.NewSetSyncBase(ps, s.is, handler)
	s.reconciler = hashsync.NewMultiPeerReconciler(
		s.syncBase, peers,
		hashsync.WithLogger(logger),
		hashsync.WithSyncPeerCount(cfg.SyncPeerCount),
		hashsync.WithMinSplitSyncPeers(cfg.MinSplitSyncPeers),
		hashsync.WithMinSplitSyncCount(cfg.MinSplitSyncCount),
		hashsync.WithMaxFullDiff(cfg.MaxFullDiff),
		hashsync.WithSyncInterval(cfg.SyncInterval),
		hashsync.WithMinCompleteFraction(cfg.MinCompleteFraction),
		hashsync.WithSplitSyncGracePeriod(time.Minute),
		hashsync.WithNoPeersRecheckInterval(cfg.NoPeersRecheckInterval))
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

func (s *P2PHashSync) ItemStore() hashsync.ItemStore {
	return s.is
}

func (s *P2PHashSync) Start() {
	s.start.Do(func() {
		var ctx context.Context
		ctx, s.cancel = context.WithCancel(context.Background())
		s.eg.Go(func() error { return s.srv.Run(ctx) })
		s.eg.Go(func() error { return s.reconciler.Run(ctx) })
		s.running.Store(true)
	})
}

func (s *P2PHashSync) Stop() {
	s.running.Store(false)
	if s.cancel != nil {
		s.cancel()
	}
	if err := s.eg.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("P2PHashSync terminated with an error", zap.Error(err))
	}
}
