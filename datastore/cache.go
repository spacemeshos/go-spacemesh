package datastore

import (
	lru "github.com/hashicorp/golang-lru"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// AtxCache holds an lru cache of ActivationTx structs of recent atx used to calculate active set size
// ideally this cache will hold the atxs created in latest epoch, on which most of active set size calculation will be
// performed.
type AtxCache struct {
	*lru.Cache
}

// NewAtxCache creates a new cache for activation transaction headers.
func NewAtxCache(size int) AtxCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Panic("could not initialize cache ", err)
	}
	return AtxCache{Cache: cache}
}

// Add adds an ActivationTx to cache.
func (bc *AtxCache) Add(id types.ATXID, atx *types.ActivationTx) {
	bc.Cache.Add(id, atx)
}

// Get gets the corresponding ActivationTx for the given id, it also returns a boolean to indicate whether the item
// was found in cache.
func (bc AtxCache) Get(id types.ATXID) (*types.ActivationTx, bool) {
	item, found := bc.Cache.Get(id)
	if !found {
		return nil, false
	}
	atx := item.(*types.ActivationTx)
	return atx, true
}
