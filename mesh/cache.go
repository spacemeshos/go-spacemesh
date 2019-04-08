package mesh

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/block"
)

type blockCache interface {
	Get(id block.BlockID) *block.MiniBlock
	put(b *block.MiniBlock)
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

func (bc BlockCache) put(b *block.MiniBlock) {
	bc.Cache.Add(b.Id, *b)
}

func (bc BlockCache) Get(id block.BlockID) *block.MiniBlock {
	item, found := bc.Cache.Get(id)
	if !found {
		return nil
	}
	blk := item.(block.MiniBlock)
	return &blk
}
