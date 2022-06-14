package fetch

import (
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
func NewHashPeersCache(size int) HashPeersCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Panic("could not initialize cache ", err)
	}
	return HashPeersCache{Cache: cache}
}

// Add adds hash peers to cache.
func (hpc *HashPeersCache) Add(hash types.Hash32, HashPeers HashPeers) {
	hpc.Cache.Add(hash, HashPeers)
}

// Get returns hash peers, it also returns a boolean to indicate whether the item
// was found in cache.
func (hpc HashPeersCache) Get(hash types.Hash32) (HashPeers, bool) {
	item, found := hpc.Cache.Get(hash)
	if !found {
		return nil, false
	}
	return item.(HashPeers), true
}

// cacheStats stores hash-to-peers cache hits & misses.
type cacheStats struct {
	// Hits is a number of successfully found hashes
	Hits int64 `json:"hits"`
	// Misses is a number of not found hashes
	Misses int64 `json:"misses"`
}

// Hit tracks hash-to-peer cache hit.
func (hpc HashPeersCache) Hit() {
	atomic.AddInt64(&hpc.stats.Hits, 1)
	metrics.LogHit()
}

// Miss tracks hash-to-peer cache miss.
func (hpc HashPeersCache) Miss() {
	atomic.AddInt64(&hpc.stats.Misses, 1)
	metrics.LogMiss()
}
