package fetch

import (
	"math/rand"
	"sync"
	"sync/atomic"

	lru "github.com/hashicorp/golang-lru"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

// HashPeersCache holds lru cache of peers to pull hash from.
type HashPeersCache struct {
	*lru.Cache
	mu    sync.Mutex
	stats cacheStats
}

// HashPeers holds registered peers for a hash.
type HashPeers map[p2p.Peer]struct{}

// NewHashPeersCache creates a new hash-to-peers cache.
func NewHashPeersCache(size int) *HashPeersCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Panic("could not initialize cache ", err)
	}
	return &HashPeersCache{Cache: cache}
}

// get returns peers for a given hash (non-thread-safe).
func (hpc *HashPeersCache) get(hash types.Hash32) (HashPeers, bool) {
	item, found := hpc.Cache.Get(hash)
	if !found {
		return nil, false
	}
	return item.(HashPeers), true
}

// getWithStats is the same as get but also updates cache stats (still non-thread-safe).
func (hpc *HashPeersCache) getWithStats(hash types.Hash32) (HashPeers, bool) {
	hashPeers, found := hpc.get(hash)
	if !found {
		hpc.miss()
		return nil, false
	}
	hpc.hit()
	return hashPeers, true
}

// add adds a peer to a hash (non-thread-safe).
func (hpc *HashPeersCache) add(hash types.Hash32, peer p2p.Peer) {
	peers, exists := hpc.get(hash)
	if !exists {
		hpc.Cache.Add(hash, HashPeers{peer: {}})
		return
	}

	peers[peer] = struct{}{}
	hpc.Cache.Add(hash, peers)
}

// Add is a thread-safe version of add.
func (hpc *HashPeersCache) Add(hash types.Hash32, peer p2p.Peer) {
	hpc.mu.Lock()
	defer hpc.mu.Unlock()

	hpc.add(hash, peer)
}

// GetRandom returns a random peer for a given hash.
func (hpc *HashPeersCache) GetRandom(hash types.Hash32, rng *rand.Rand) (p2p.Peer, bool) {
	hpc.mu.Lock()
	defer hpc.mu.Unlock()

	hashPeersMap, exists := hpc.getWithStats(hash)
	if !exists {
		return p2p.NoPeer, false
	}
	n := rng.Intn(len(hashPeersMap)) + 1
	i := 0
	for peer := range hashPeersMap {
		i++
		if i == n {
			return peer, true
		}
	}
	return p2p.NoPeer, false
}

// RegisterPeerHashes registers provided peer for a list of hashes.
func (hpc *HashPeersCache) RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32) {
	if len(hashes) == 0 {
		return
	}
	if p2p.IsNoPeer(peer) {
		return
	}
	for _, hash := range hashes {
		hpc.Add(hash, peer)
	}
	return
}

// AddPeersFromHash adds peers from one hash to others.
func (hpc *HashPeersCache) AddPeersFromHash(fromHash types.Hash32, toHashes []types.Hash32) {
	hpc.mu.Lock()
	defer hpc.mu.Unlock()

	peers, exists := hpc.getWithStats(fromHash)
	if !exists {
		return
	}
	for peer := range peers {
		for _, hash := range toHashes {
			hpc.add(hash, peer)
		}
	}
	return
}

// cacheStats stores hash-to-peers cache hits & misses.
type cacheStats struct {
	hits   uint64
	misses uint64
}

func (hpc *HashPeersCache) hit() {
	atomic.AddUint64(&hpc.stats.hits, 1)
	metrics.LogHit()
	metrics.LogHitRate(atomic.LoadUint64(&hpc.stats.hits), atomic.LoadUint64(&hpc.stats.misses))
}

func (hpc *HashPeersCache) miss() {
	atomic.AddUint64(&hpc.stats.misses, 1)
	metrics.LogMiss()
	metrics.LogHitRate(atomic.LoadUint64(&hpc.stats.hits), atomic.LoadUint64(&hpc.stats.misses))
}
