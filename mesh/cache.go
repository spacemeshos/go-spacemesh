package mesh

import (
	"github.com/hashicorp/golang-lru"
	"github.com/prometheus/common/log"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type blockCache interface {
	Get(id types.BlockID) *types.Block
	put(b *types.Block)
	Cap() int
	Close()
}

type BlockCache struct {
	cap int
	blockCache
	*lru.Cache
}

func NewBlockCache(cap int) BlockCache {
	cache, err := lru.New(cap)
	if err != nil {
		log.Fatal("could not initialize cache ", err)
	}
	return BlockCache{Cache: cache, cap: cap}
}

func (bc BlockCache) Cap() int {
	return bc.cap
}

func (bc BlockCache) put(b *types.Block) {
	bc.Cache.Add(b.ID(), *b)
}

func (bc BlockCache) Get(id types.BlockID) *types.Block {
	item, found := bc.Cache.Get(id)
	if !found {
		return nil
	}
	blk := item.(types.Block)
	return &blk
}
