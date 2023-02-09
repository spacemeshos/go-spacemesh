package fetch

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

// getCachedEntry is a thread-safe cache get helper.
func getCachedEntry(cache *HashPeersCache, hash types.Hash32) (HashPeers, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	return cache.get(hash)
}

func TestAdd(t *testing.T) {
	t.Parallel()
	t.Run("1Hash3Peers", func(t *testing.T) {
		cache := NewHashPeersCache(10)
		hash := types.RandomHash()
		peer1 := p2p.Peer("test_peer_1")
		peer2 := p2p.Peer("test_peer_2")
		peer3 := p2p.Peer("test_peer_3")
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			cache.Add(hash, peer1)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash, peer2)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash, peer3)
		}()
		wg.Wait()
		hashPeers, _ := getCachedEntry(cache, hash)
		require.Equal(t, 3, len(hashPeers))
	})
	t.Run("2Hashes1Peer", func(t *testing.T) {
		cache := NewHashPeersCache(10)
		hash1 := types.RandomHash()
		hash2 := types.RandomHash()
		peer := p2p.Peer("test_peer")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cache.Add(hash1, peer)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash2, peer)
		}()
		wg.Wait()
		hash1Peers, _ := getCachedEntry(cache, hash1)
		require.Equal(t, 1, len(hash1Peers))
		hash2Peers, _ := getCachedEntry(cache, hash2)
		require.Equal(t, 1, len(hash2Peers))
	})
}

func TestGetRandom(t *testing.T) {
	t.Parallel()
	t.Run("1Hash3Peers", func(t *testing.T) {
		cache := NewHashPeersCache(10)
		hash := types.RandomHash()
		peer1 := p2p.Peer("test_peer_1")
		peer2 := p2p.Peer("test_peer_2")
		peer3 := p2p.Peer("test_peer_3")
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			cache.Add(hash, peer1)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash, peer2)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash, peer3)
		}()
		wg.Wait()
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		peer, exists := cache.GetRandom(hash, datastore.TXDB, rng)
		require.Equal(t, true, exists)
		require.Contains(t, []p2p.Peer{peer1, peer2, peer3}, peer)
	})
	t.Run("2Hashes1Peer", func(t *testing.T) {
		cache := NewHashPeersCache(10)
		hash1 := types.RandomHash()
		hash2 := types.RandomHash()
		peer := p2p.Peer("test_peer")
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			cache.Add(hash1, peer)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash2, peer)
		}()
		wg.Wait()
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		randomPeer, exists := cache.GetRandom(hash1, datastore.TXDB, rng)
		require.Equal(t, true, exists)
		require.Equal(t, peer, randomPeer)
		randomPeer, exists = cache.GetRandom(hash2, datastore.TXDB, rng)
		require.Equal(t, true, exists)
		require.Equal(t, peer, randomPeer)
	})
}

func TestRegisterPeerHashes(t *testing.T) {
	t.Parallel()
	t.Run("1Hash2Peers", func(t *testing.T) {
		cache := NewHashPeersCache(10)
		hash1 := types.RandomHash()
		hash2 := types.RandomHash()
		hash3 := types.RandomHash()
		peer1 := p2p.Peer("test_peer_1")
		cache.RegisterPeerHashes(peer1, []types.Hash32{hash1, hash2, hash3})
		hash1Peers, _ := getCachedEntry(cache, hash1)
		require.Equal(t, 1, len(hash1Peers))
		hash2Peers, _ := getCachedEntry(cache, hash2)
		require.Equal(t, 1, len(hash2Peers))
		hash3Peers, _ := getCachedEntry(cache, hash3)
		require.Equal(t, 1, len(hash3Peers))
	})
}

func TestRace(t *testing.T) {
	cache := NewHashPeersCache(10)
	hash := types.RandomHash()
	peer1 := p2p.Peer("test_peer_1")
	peer2 := p2p.Peer("test_peer_2")
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		cache.Add(hash, peer1)
	}()
	go func() {
		defer wg.Done()
		cache.GetRandom(hash, datastore.TXDB, rng)
	}()
	go func() {
		defer wg.Done()
		cache.Add(hash, peer2)
	}()
	go func() {
		defer wg.Done()
		cache.GetRandom(hash, datastore.TXDB, rng)
	}()
	wg.Wait()
}
