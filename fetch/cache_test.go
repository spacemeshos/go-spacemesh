package fetch_test

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

func TestCache(t *testing.T) {
	t.Parallel()
	t.Run("1Hash3Peers", func(t *testing.T) {
		cache := fetch.NewHashPeersCache(10)
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
		hashPeers, _ := cache.Get(hash)
		require.Equal(t, 3, len(hashPeers))
	})
	t.Run("2Hashes1Peer", func(t *testing.T) {
		cache := fetch.NewHashPeersCache(10)
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
		hash1Peers, _ := cache.Get(hash1)
		require.Equal(t, 1, len(hash1Peers))
		hash2Peers, _ := cache.Get(hash2)
		require.Equal(t, 1, len(hash2Peers))
	})
}

func TestRace(t *testing.T) {
	t.Parallel()
	t.Run("AddAndGetRandom", func(t *testing.T) {
		cache := fetch.NewHashPeersCache(10)
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
			cache.GetRandom(hash, rng)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash, peer2)
		}()
		go func() {
			defer wg.Done()
			cache.GetRandom(hash, rng)
		}()
		wg.Wait()
	})
	t.Run("AddPeersFromHashAndGetRandom", func(t *testing.T) {
		cache := fetch.NewHashPeersCache(10)
		hash1 := types.RandomHash()
		hash2 := types.RandomHash()
		peer1 := p2p.Peer("test_peer_1")
		peer2 := p2p.Peer("test_peer_2")
		peer3 := p2p.Peer("test_peer_3")
		peer4 := p2p.Peer("test_peer_4")
		cache.Add(hash1, peer1)
		cache.Add(hash1, peer2)
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		var wg sync.WaitGroup
		wg.Add(5)
		go func() {
			defer wg.Done()
			cache.AddPeersFromHash(hash1, []types.Hash32{hash2})
		}()
		go func() {
			defer wg.Done()
			cache.GetRandom(hash1, rng)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash2, peer3)
		}()
		go func() {
			defer wg.Done()
			cache.GetRandom(hash2, rng)
		}()
		go func() {
			defer wg.Done()
			cache.Add(hash2, peer4)
		}()
		wg.Wait()
	})
}
