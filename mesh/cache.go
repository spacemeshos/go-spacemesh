package mesh

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type blockCache interface {
	Get(id types.BlockID) *types.Block
	put(b *types.Block)
	Close()
}

type BlockCache struct {
	blockCache
	*lru.Cache
}

func NewBlockCache(size int) BlockCache {
	cache, err := lru.New(size)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return BlockCache{Cache: cache}
}

func (bc BlockCache) put(b *types.Block) {
	bc.Cache.Add(b.Id(), *b)
}

func (bc BlockCache) Get(id types.BlockID) *types.Block {
	item, found := bc.Cache.Get(id)
	if !found {
		return nil
	}
	blk := item.(types.Block)
	return &blk
}
