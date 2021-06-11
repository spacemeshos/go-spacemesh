package activation

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// TotalWeightCache holds an lru cache of the total weight per view hash.
type TotalWeightCache struct {
	*lru.Cache
}

// NewTotalWeightCache creates a cache for total weight per view hash.
func NewTotalWeightCache(size int) TotalWeightCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return TotalWeightCache{Cache: cache}
}

// Add adds a viewHash and a totalWeight that was calculated for this viewHash.
func (bc *TotalWeightCache) Add(viewHash types.Hash12, totalWeight uint64) {
	bc.Cache.Add(viewHash, totalWeight)
}

// Get returns the stored totalWeight for the provided viewHash.
func (bc TotalWeightCache) Get(viewHash types.Hash12) (uint64, bool) {
	item, found := bc.Cache.Get(viewHash)
	if !found {
		return 0, false
	}
	totalWeight := item.(uint64)
	return totalWeight, true
}

// AtxCache holds an lru cache of ActivationTxHeader structs of recent atx used to calculate active set size
// ideally this cache will hold the atxs created in latest epoch, on which most of active set size calculation will be
// performed
type AtxCache struct {
	*lru.Cache
}

// NewAtxCache creates a new cache for activation transaction headers
func NewAtxCache(size int) AtxCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return AtxCache{Cache: cache}
}

// Add adds an activationTxHeader to cache
func (bc *AtxCache) Add(id types.ATXID, atxHeader *types.ActivationTxHeader) {
	bc.Cache.Add(id, atxHeader)
}

// Get gets the corresponding Atx header to the given id, it also returns a boolean to indicate whether the item
// was found in cache
func (bc AtxCache) Get(id types.ATXID) (*types.ActivationTxHeader, bool) {
	item, found := bc.Cache.Get(id)
	if !found {
		return nil, false
	}
	atxHeader := item.(*types.ActivationTxHeader)
	return atxHeader, true
}
