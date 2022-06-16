package fetch

import (
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

// ToList converts hash peers map to a list.
func (hp HashPeers) ToList() []p2p.Peer {
	result := make([]p2p.Peer, 0, len(hp))
	for k := range hp {
		result = append(result, k)
	}
	return result
}

// NewHashPeersCache creates a new hash-to-peers cache.
func NewHashPeersCache(size int) *HashPeersCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Panic("could not initialize cache ", err)
	}
	return &HashPeersCache{Cache: cache}
}

// Add adds peer to a hash.
func (hpc *HashPeersCache) Add(hash types.Hash32, peer p2p.Peer) {
	hpc.mu.Lock()
	defer hpc.mu.Unlock()

	peers, exists := hpc.get(hash)
	if !exists {
		hpc.Cache.Add(hash, HashPeers{peer: {}})
		return
	}

	peers[peer] = struct{}{}
	hpc.Cache.Add(hash, peers)
}

// Get returns hash peers, it also returns a boolean to indicate whether the item
// was found in cache.
func (hpc *HashPeersCache) Get(hash types.Hash32) (HashPeers, bool) {
	hashPeers, found := hpc.get(hash)
	if !found {
		hpc.miss()
		return nil, false
	}
	hpc.hit()
	return hashPeers, true
}

// get is the same as Get but doesn't affect cache stats.
func (hpc *HashPeersCache) get(hash types.Hash32) (HashPeers, bool) {
	item, found := hpc.Cache.Get(hash)
	if !found {
		return nil, false
	}
	return item.(HashPeers), true
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
