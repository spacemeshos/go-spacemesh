package eligibility

import (
	"context"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/singleflight"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type cachedWeights struct {
	mu           sync.Mutex
	minerWeights map[types.EpochID]map[types.NodeID]uint64

	sf           singleflight.Group
	epochWeights *lru.Cache[types.EpochID, uint64]

	weightsSvc weights
}

func NewCachedWeights(weightsSvc weights) *cachedWeights {
	epochWeightsCache, err := lru.New[types.EpochID, uint64](2)
	if err != nil {
		panic("failed to create epoch weights cache")
	}
	return &cachedWeights{
		minerWeights: make(map[types.EpochID]map[types.NodeID]uint64, 2),
		epochWeights: epochWeightsCache,
		weightsSvc:   weightsSvc,
	}
}

func (c *cachedWeights) MinerWeight(ctx context.Context, epoch types.EpochID, node types.NodeID) (uint64, error) {
	c.mu.Lock()
	if cache, ok := c.minerWeights[epoch]; ok {
		if w, ok := cache[node]; ok {
			c.mu.Unlock()
			return w, nil
		}
	}
	c.mu.Unlock()

	w, err := c.weightsSvc.MinerWeight(ctx, epoch, node)
	if err != nil {
		return 0, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if cache, ok := c.minerWeights[epoch]; ok {
		cache[node] = w
	} else {
		c.minerWeights[epoch] = make(map[types.NodeID]uint64, 100)
		c.minerWeights[epoch][node] = w

		// evict old epoch as it's not needed anymore
		delete(c.minerWeights, epoch-2)
	}
	return w, nil
}

func (c *cachedWeights) TotalWeight(ctx context.Context, epoch types.EpochID) (uint64, error) {
	res, err, _ := c.sf.Do(epoch.String(), func() (any, error) {
		if w, ok := c.epochWeights.Get(epoch); ok {
			return w, nil
		}
		w, err := c.weightsSvc.TotalWeight(ctx, epoch)
		if err == nil {
			c.epochWeights.Add(epoch, w)
		}
		return w, err
	})
	return res.(uint64), err
}
