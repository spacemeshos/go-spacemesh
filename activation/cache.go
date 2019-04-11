package activation

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/common"
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

func (bc *ActivesetCache) put(view common.Hash, setSize uint32) {
	bc.Cache.Add(view, setSize)
}

func (bc ActivesetCache) Get(view common.Hash) (uint32, bool) {
	item, found := bc.Cache.Get(view)
	if !found {
		return 0, false
	}
	blk := item.(uint32)
	return blk, true
}
