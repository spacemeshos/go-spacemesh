package sync2_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sync2"
	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

type addedKey struct {
	// The fields are actually used to make sure each key is synced just once between
	// each pair of peers.
	//nolint:unused
	fromPeer, toPeer p2p.Peer
	//nolint:unused
	key string
}

type fakeHandler struct {
	mtx         *sync.Mutex
	localPeerID p2p.Peer
	synced      map[addedKey]struct{}
	committed   map[string]struct{}
}

func (fh *fakeHandler) Receive(k rangesync.KeyBytes, peer p2p.Peer) (bool, error) {
	fh.mtx.Lock()
	defer fh.mtx.Unlock()
	ak := addedKey{
		toPeer: fh.localPeerID,
		key:    string(k),
	}
	fh.synced[ak] = struct{}{}
	return true, nil
}

func (fh *fakeHandler) Commit(peer p2p.Peer, base, new multipeer.OrderedSet) error {
	fh.mtx.Lock()
	defer fh.mtx.Unlock()
	for k := range fh.synced {
		fh.committed[k.key] = struct{}{}
	}
	clear(fh.synced)
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
				ps.Add(mesh.Hosts()[m].ID())
			}
		}
		cfg := sync2.DefaultConfig()
		cfg.EnableActiveSync = true
		cfg.SyncInterval = 100 * time.Millisecond
		host := mesh.Hosts()[n]
		handlers[n] = &fakeHandler{
			mtx:         &mtx,
			localPeerID: host.ID(),
			synced:      make(map[addedKey]struct{}),
			committed:   make(map[string]struct{}),
		}
		os := multipeer.NewDumbHashSet()
		d := rangesync.NewDispatcher(logger)
		srv := d.SetupServer(host, "sync2test", server.WithLog(logger))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		eg.Go(func() error { return srv.Run(ctx) })
		hs[n] = sync2.NewP2PHashSync(
			logger.Named(fmt.Sprintf("node%d", n)),
			d, "test", os, keyLen, maxDepth, ps, handlers[n], cfg, srv)
		require.NoError(t, hs[n].Load())
		is := hs[n].Set().(*multipeer.DumbSet)
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
			os := hsync.Set().Copy(false)
			for _, k := range handlers[n].committedItems() {
				os.(*multipeer.DumbSet).AddUnchecked(k)
			}
			empty, err := os.Empty()
			require.NoError(t, err)
			if empty {
				return false
			}
			k, err := os.Items().First()
			require.NoError(t, err)
			info, err := os.GetRangeInfo(k, k)
			require.NoError(t, err)
			if info.Count < numHashes {
				return false
			}
		}
		return true
	}, 30*time.Second, 300*time.Millisecond)

	for n, hsync := range hs {
		hsync.Stop()
		os := hsync.Set().Copy(false)
		for _, k := range handlers[n].committedItems() {
			os.(*multipeer.DumbSet).AddUnchecked(k)
		}
		actualItems, err := os.Items().Collect()
		require.NoError(t, err)
		require.ElementsMatch(t, initialSet, actualItems)
	}
}
