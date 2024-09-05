package sync2

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

func TestP2P(t *testing.T) {
	const (
		numNodes  = 4
		numHashes = 100
	)
	logger := zaptest.NewLogger(t)
	mesh, err := mocknet.FullMeshConnected(numNodes)
	require.NoError(t, err)
	type addedKey struct {
		fromPeer, toPeer p2p.Peer
		key              string
	}
	var mtx sync.Mutex
	synced := make(map[addedKey]struct{})
	hs := make([]*P2PHashSync, numNodes)
	initialSet := make([]types.KeyBytes, numHashes)
	for n := range initialSet {
		initialSet[n] = types.RandomKeyBytes(32)
	}
	for n := range hs {
		ps := peers.New()
		for m := 0; m < numNodes; m++ {
			if m != n {
				ps.Add(mesh.Hosts()[m].ID())
			}
		}
		cfg := DefaultConfig()
		cfg.SyncInterval = 100 * time.Millisecond
		host := mesh.Hosts()[n]
		handler := func(ctx context.Context, k types.Ordered, peer p2p.Peer) error {
			mtx.Lock()
			defer mtx.Unlock()
			ak := addedKey{
				fromPeer: peer,
				toPeer:   host.ID(),
				key:      string(k.(types.KeyBytes)),
			}
			synced[ak] = struct{}{}
			return nil
		}
		os := rangesync.NewDumbHashSet(true)
		hs[n] = NewP2PHashSync(
			logger.Named(fmt.Sprintf("node%d", n)),
			host, os, 32, 24, "sync2test", ps, handler, cfg)
		if n == 0 {
			is := hs[n].Set()
			for _, h := range initialSet {
				is.Add(context.Background(), h)
			}
		}
		require.False(t, hs[n].Synced())
		hs[n].Start()
	}

	require.Eventually(t, func() bool {
		for _, hsync := range hs {
			// use a snapshot to avoid races
			if !hsync.Synced() {
				return false
			}
			os := hsync.Set().Copy()
			empty, err := os.Empty(context.Background())
			require.NoError(t, err)
			if empty {
				return false
			}
			seq, err := os.Items(context.Background())
			require.NoError(t, err)
			k, err := seq.First()
			require.NoError(t, err)
			info, err := os.GetRangeInfo(context.Background(), k, k, -1)
			require.NoError(t, err)
			if info.Count < numHashes {
				return false
			}
		}
		return true
	}, 30*time.Second, 300*time.Millisecond)

	for _, hsync := range hs {
		hsync.Stop()
		actualItems, err := rangesync.CollectSetItems[types.KeyBytes](context.Background(), hsync.Set())
		require.NoError(t, err)
		require.ElementsMatch(t, initialSet, actualItems)
	}
}

// TODO: make sure all the keys have passed through the handler before being added
