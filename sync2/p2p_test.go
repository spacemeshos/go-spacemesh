package sync2_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type fakeHandler struct {
	mtx       *sync.Mutex
	committed map[string]struct{}
}

func (fh *fakeHandler) Commit(
	ctx context.Context,
	peer p2p.Peer,
	base rangesync.OrderedSet,
	received rangesync.SeqResult,
) error {
	fh.mtx.Lock()
	defer fh.mtx.Unlock()
	for k := range received.Seq {
		fh.committed[string(k)] = struct{}{}
	}
	return nil
}

func (fh *fakeHandler) committedItems() (items []rangesync.KeyBytes) {
	fh.mtx.Lock()
	defer fh.mtx.Unlock()
	for k := range fh.committed {
		items = append(items, rangesync.KeyBytes(k))
	}
	return items
}

func TestP2P(t *testing.T) {
	const (
		numNodes  = 4
		numHashes = 100
		keyLen    = 32
		maxDepth  = 24
	)
	logger := zaptest.NewLogger(t)
	mesh, err := mocknet.FullMeshConnected(numNodes)
	require.NoError(t, err)
	hs := make([]*sync2.P2PHashSync, numNodes)
	handlers := make([]*fakeHandler, numNodes)
	initialSet := make([]rangesync.KeyBytes, numHashes)
	for n := range initialSet {
		initialSet[n] = rangesync.RandomKeyBytes(32)
	}
	var eg errgroup.Group
	var mtx sync.Mutex
	defer eg.Wait()
	for n := range hs {
		ps := peers.New()
		for m := 0; m < numNodes; m++ {
			if m != n {
				ps.Add(mesh.Hosts()[m].ID(), func() []protocol.ID {
					return []protocol.ID{multipeer.Protocol}
				})
			}
		}
		cfg := sync2.DefaultConfig()
		cfg.SyncInterval = 100 * time.Millisecond
		cfg.MaxDepth = maxDepth
		host := mesh.Hosts()[n]
		handlers[n] = &fakeHandler{
			mtx:       &mtx,
			committed: make(map[string]struct{}),
		}
		var os rangesync.DumbSet
		d := rangesync.NewDispatcher(logger)
		srv := d.SetupServer(host, "sync2test", server.WithLog(logger))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		eg.Go(func() error { return srv.Run(ctx) })
		hs[n], err = sync2.NewP2PHashSync(
			logger.Named(fmt.Sprintf("node%d", n)),
			d, "test", &os, keyLen, ps, handlers[n], cfg, true)
		require.NoError(t, err)
		require.NoError(t, hs[n].Load())
		is := hs[n].Set().(*rangesync.DumbSet)
		is.SetAllowMultiReceive(true)
		if n == 0 {
			for _, h := range initialSet {
				is.AddUnchecked(h)
			}
		}
		require.False(t, hs[n].Synced())
		hs[n].Start()
	}

	require.Eventually(t, func() bool {
		for n, hsync := range hs {
			// use a snapshot to avoid races
			if !hsync.Synced() {
				return false
			}
			r := true
			require.NoError(t, hsync.Set().WithCopy(
				context.Background(),
				func(os rangesync.OrderedSet) error {
					for _, k := range handlers[n].committedItems() {
						os.(*rangesync.DumbSet).AddUnchecked(k)
					}
					empty, err := os.Empty()
					require.NoError(t, err)
					if empty {
						r = false
					} else {
						k, err := os.Items().First()
						require.NoError(t, err)
						info, err := os.GetRangeInfo(k, k)
						require.NoError(t, err)
						if info.Count < numHashes {
							r = false
						}
					}
					return nil
				}))
			if !r {
				return false
			}
		}
		return true
	}, 30*time.Second, 300*time.Millisecond)

	for n, hsync := range hs {
		hsync.Stop()
		require.NoError(t, hsync.Set().WithCopy(
			context.Background(),
			func(os rangesync.OrderedSet) error {
				for _, k := range handlers[n].committedItems() {
					os.(*rangesync.DumbSet).AddUnchecked(k)
				}
				actualItems, err := os.Items().Collect()
				require.NoError(t, err)
				require.ElementsMatch(t, initialSet, actualItems)
				return nil
			}))
	}
}

func TestConfigValidation(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		obs, observedLogs := observer.New(zapcore.ErrorLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, obs)
			},
		)))
		cfg := sync2.DefaultConfig()
		require.True(t, cfg.Validate(logger))
		require.Equal(t, 0, observedLogs.Len(), "expected 0 log messages")
	})
	t.Run("faulty", func(t *testing.T) {
		for _, tc := range []struct {
			cfg     func(cfg *sync2.Config)
			expErrs []string
		}{
			{
				cfg: func(cfg *sync2.Config) {
					*cfg = sync2.Config{}
				},
				expErrs: []string{
					"max-send-range must be positive",
					"item-chunk-size must be positive",
					"sync-peer-count must be positive",
					"min-split-sync-peers must be positive",
					"min-split-sync-count must be positive",
					"sync-interval must be positive",
					"retry-interval must be positive",
					"no-peers-recheck-interval must be positive",
					"split-sync-grace-period must be positive",
					"min-full-syncedness-count must be positive",
					"full-syncedness-period must be positive",
					"max-depth must be at least 1",
					"batch-size must be at least 1",
					"max-attempts must be at least 1",
				},
			},
			{
				cfg: func(cfg *sync2.Config) {
					cfg.SyncInterval = 0
				},
				expErrs: []string{
					"sync-interval must be positive",
				},
			},
			{
				cfg: func(cfg *sync2.Config) {
					cfg.RangeSetReconcilerConfig.MaxReconcDiff = 2
				},
				expErrs: []string{
					"bad max-reconc-diff, should be within [0, 1] interval",
				},
			},
			{
				cfg: func(cfg *sync2.Config) {
					cfg.MultiPeerReconcilerConfig.MinCompleteFraction = 3
				},
				expErrs: []string{
					"min-complete-fraction must be in [0, 1] interval",
				},
			},
		} {
			t.Run("", func(t *testing.T) {
				obs, observedLogs := observer.New(zapcore.ErrorLevel)
				logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
					func(core zapcore.Core) zapcore.Core {
						return zapcore.NewTee(core, obs)
					},
				)))
				cfg := sync2.DefaultConfig()
				tc.cfg(&cfg)
				require.False(t, cfg.Validate(logger))
				var msgs []string
				for _, e := range observedLogs.All() {
					msgs = append(msgs, e.Message)
				}
				require.ElementsMatch(t, tc.expErrs, msgs)
			})
		}
	})
}
