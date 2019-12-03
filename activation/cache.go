package activation

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/metrics"
)

type Cache interface {
	Add(key, value interface{}) (evicted bool)
	Get(key interface{}) (value interface{}, ok bool)
}

type ActivesetCache struct {
	Cache
}

func NewActivesetCache(size int) ActivesetCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return ActivesetCache{Cache: metrics.NewMeteredCache(cache, "activation", "activeset", "cache of active set size calc ", nil)}
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
	Cache
}

func NewAtxCache(size int) AtxCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return AtxCache{Cache: metrics.NewMeteredCache(cache, "activation", "atx", "new atx headers cache", nil)}
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
