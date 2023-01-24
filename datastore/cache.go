package datastore

import (
	lru "github.com/hashicorp/golang-lru"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// NewAtxCache creates a new cache for activation transaction headers.
func NewAtxCache(size int) *Cache[types.ATXID, types.ActivationTxHeader] {
	return NewCache[types.ATXID, types.ActivationTxHeader](size)
}

// NewMalfeasanceCache creates a cache of MalfeasanceProof of recent malicious identities.
func NewMalfeasanceCache(size int) *Cache[types.NodeID, types.MalfeasanceProof] {
	return NewCache[types.NodeID, types.MalfeasanceProof](size)
}

// Cache holds a lru cache of recent T instances.
type Cache[K, V any] struct {
	lru *lru.Cache
}

func NewCache[K, V any](size int) *Cache[K, V] {
	lruCache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return &Cache[K, V]{lru: lruCache}
}

// Add adds a key-value pair to cache.
func (c *Cache[K, V]) Add(key K, val *V) {
	c.lru.Add(key, val)
}

// Get gets the corresponding value for the given key.
// it also returns a boolean to indicate whether the item was found in cache.
func (c *Cache[K, V]) Get(key K) (*V, bool) {
	item, found := c.lru.Get(key)
	if !found {
		return nil, false
	}
	return item.(*V), true
}
