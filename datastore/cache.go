package datastore

import (
	lru "github.com/hashicorp/golang-lru"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// AtxCache holds an lru cache of ActivationTxHeader structs of recent atx used to calculate active set size
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

// Add adds an ActivationTxHeader to cache.
func (bc *AtxCache) Add(id types.ATXID, atxHeader *types.ActivationTxHeader) {
	bc.Cache.Add(id, atxHeader)
}

// Get gets the corresponding ActivationTxHeader for the given id, it also returns a boolean to indicate whether the item
// was found in cache.
func (bc AtxCache) Get(id types.ATXID) (*types.ActivationTxHeader, bool) {
	item, found := bc.Cache.Get(id)
	if !found {
		return nil, false
	}
	atxHeader := item.(*types.ActivationTxHeader)
	return atxHeader, true
}
