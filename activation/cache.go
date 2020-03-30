package activation

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// ActivesetCache holds an lru cache of the active set size for a view hash.
type ActivesetCache struct {
	*lru.Cache
}

// NewActivesetCache creates a cache for Active set size
func NewActivesetCache(size int) ActivesetCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return ActivesetCache{Cache: cache}
}

// Add adds a view hash and a set size that was calculated for this view
func (bc *ActivesetCache) Add(view types.Hash12, setSize uint32) {
	bc.Cache.Add(view, setSize)
}

// Get returns the stored active set size for the provided view hash
func (bc ActivesetCache) Get(view types.Hash12) (uint32, bool) {
	item, found := bc.Cache.Get(view)
	if !found {
		return 0, false
	}
	blk := item.(uint32)
	return blk, true
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
