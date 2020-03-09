package activation

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type ActivesetCache struct {
	*lru.Cache
}

func NewActivesetCache(size int) ActivesetCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return ActivesetCache{Cache: cache}
}

func (bc *ActivesetCache) Add(view types.Hash12, setSize uint32) {
	bc.Cache.Add(view, setSize)
}

func (bc ActivesetCache) Get(view types.Hash12) (uint32, bool) {
	item, found := bc.Cache.Get(view)
	if !found {
		return 0, false
	}
	blk := item.(uint32)
	return blk, true
}

type AtxCache struct {
	*lru.Cache
}

func NewAtxCache(size int) AtxCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return AtxCache{Cache: cache}
}

func (bc *AtxCache) Add(id types.AtxId, atxHeader *types.ActivationTxHeader) {
	bc.Cache.Add(id, atxHeader)
}

func (bc AtxCache) Get(id types.AtxId) (*types.ActivationTxHeader, bool) {
	item, found := bc.Cache.Get(id)
	if !found {
		return nil, false
	}
	atxHeader := item.(*types.ActivationTxHeader)
	return atxHeader, true
}
