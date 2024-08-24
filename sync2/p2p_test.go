package sync2

import (
	"context"
	"sync"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
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
		key              hashsync.Ordered
	}
	var mtx sync.Mutex
	synced := make(map[addedKey]struct{})
	hs := make([]*P2PHashSync, numNodes)
	initialSet := make([]types.Hash32, numHashes)
	for n := range initialSet {
		initialSet[n] = types.RandomHash()
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
		handler := func(ctx context.Context, k hashsync.Ordered, peer p2p.Peer) error {
			mtx.Lock()
			defer mtx.Unlock()
			ak := addedKey{
				fromPeer: peer,
				toPeer:   host.ID(),
				key:      k,
			}
			synced[ak] = struct{}{}
			return nil
		}
		hs[n] = NewP2PHashSync(logger, host, "sync2test", ps, handler, cfg)
		if n == 0 {
			is := hs[n].ItemStore()
			for _, h := range initialSet {
				is.Add(context.Background(), h)
			}
		}
		hs[n].Start()
	}

	require.Eventually(t, func() bool {
		for _, hsync := range hs {
			// use a snapshot to avoid races
			is := hsync.ItemStore().Copy()
			it, err := is.Min(context.Background())
			require.NoError(t, err)
			if it == nil {
				return false
			}
			k, err := it.Key()
			require.NoError(t, err)
			info, err := is.GetRangeInfo(context.Background(), nil, k, k, -1)
			require.NoError(t, err)
			if info.Count < numHashes {
				return false
			}
		}
		return true
	}, 30*time.Second, 300*time.Millisecond)

	for _, hsync := range hs {
		hsync.Stop()
		actualItems, err := hashsync.CollectStoreItems[types.Hash32](hsync.ItemStore())
		require.NoError(t, err)
		require.ElementsMatch(t, initialSet, actualItems)
	}
}

// TODO: make sure all the keys have passed through the handler before being added
