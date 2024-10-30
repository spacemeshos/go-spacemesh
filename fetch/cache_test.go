package fetch

import (
	"encoding/binary"
	"math/rand/v2"
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
		cache, err := NewHashPeersCache(10)
		require.NoError(t, err)
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
		require.Len(t, hashPeers, 3)
	})
	t.Run("2Hashes1Peer", func(t *testing.T) {
		cache, err := NewHashPeersCache(10)
		require.NoError(t, err)
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
		require.Len(t, hash1Peers, 1)
		hash2Peers, _ := getCachedEntry(cache, hash2)
		require.Len(t, hash2Peers, 1)
	})
}

func TestGetRandom(t *testing.T) {
	t.Parallel()
	t.Run("no hash peers", func(t *testing.T) {
		cache, err := NewHashPeersCache(10)
		require.NoError(t, err)
		hash := types.RandomHash()
		var seed [32]byte
		binary.LittleEndian.PutUint64(seed[:], uint64(time.Now().UnixNano()))
		rng := rand.New(rand.NewChaCha8(seed))
		peers := cache.GetRandom(hash, datastore.TXDB, rng)
		require.Empty(t, peers)
	})
	t.Run("1Hash3Peers", func(t *testing.T) {
		cache, err := NewHashPeersCache(10)
		require.NoError(t, err)
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
		var seed [32]byte
		binary.LittleEndian.PutUint64(seed[:], uint64(time.Now().UnixNano()))
		rng := rand.New(rand.NewChaCha8(seed))
		peers := cache.GetRandom(hash, datastore.TXDB, rng)
		require.ElementsMatch(t, []p2p.Peer{peer1, peer2, peer3}, peers)
	})
	t.Run("2Hashes1Peer", func(t *testing.T) {
		cache, err := NewHashPeersCache(10)
		require.NoError(t, err)
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
		var seed [32]byte
		binary.LittleEndian.PutUint64(seed[:], uint64(time.Now().UnixNano()))
		rng := rand.New(rand.NewChaCha8(seed))
		randomPeers := cache.GetRandom(hash1, datastore.TXDB, rng)
		require.Equal(t, []p2p.Peer{peer}, randomPeers)
		randomPeers = cache.GetRandom(hash2, datastore.TXDB, rng)
		require.Equal(t, []p2p.Peer{peer}, randomPeers)
	})
}

func TestRegisterPeerHashes(t *testing.T) {
	t.Parallel()
	t.Run("1Hash2Peers", func(t *testing.T) {
		cache, err := NewHashPeersCache(10)
		require.NoError(t, err)
		hash1 := types.RandomHash()
		hash2 := types.RandomHash()
		hash3 := types.RandomHash()
		peer1 := p2p.Peer("test_peer_1")
		cache.RegisterPeerHashes(peer1, []types.Hash32{hash1, hash2, hash3})
		hash1Peers, _ := getCachedEntry(cache, hash1)
		require.Len(t, hash1Peers, 1)
		hash2Peers, _ := getCachedEntry(cache, hash2)
		require.Len(t, hash2Peers, 1)
		hash3Peers, _ := getCachedEntry(cache, hash3)
		require.Len(t, hash3Peers, 1)
	})
}

func TestRace(t *testing.T) {
	cache, err := NewHashPeersCache(10)
	require.NoError(t, err)
	hash := types.RandomHash()
	peer1 := p2p.Peer("test_peer_1")
	peer2 := p2p.Peer("test_peer_2")
	var seed [32]byte
	binary.LittleEndian.PutUint64(seed[:], uint64(time.Now().UnixNano()))
	rng := rand.New(rand.NewChaCha8(seed))
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
