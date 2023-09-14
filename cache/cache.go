package cache

import (
	"bytes"
	"sort"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type ATXData struct {
	Weight             uint64
	BaseHeight, Height uint64
	Nonce              types.VRFPostIndex
	Malicious          bool
}

type Opt func(*Cache)

func WithCapacity(capacity types.EpochID) Opt {
	return func(cache *Cache) {
		cache.capacity = capacity
	}
}

func New(opts ...Opt) *Cache {
	cache := &Cache{
		capacity: 2,
		epochs:   map[types.EpochID]epochCache{},
	}
	for _, opt := range opts {
		opt(cache)
	}
	return cache
}

type Cache struct {
	// number of epochs to keep
	// capacity is used for informational purposes.
	capacity types.EpochID

	mu     sync.RWMutex
	epochs map[types.EpochID]epochCache
}

type epochCache struct {
	atxs       map[types.ATXID]*ATXData
	identities map[types.NodeID][]types.ATXID
}

func (c *Cache) Capacity() types.EpochID {
	return c.capacity
}

// Evict supposed to be called when application knows that keeping epoch in memory is not useful.
//
// Good heuristic is when layer from higher epoch was applied, at that point it is unlikely
// that we will see a lot of data from past epochs.
func (c *Cache) Evict(epoch types.EpochID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.epochs, epoch)
}

func (c *Cache) Add(epoch types.EpochID, node types.NodeID, atx types.ATXID, data *ATXData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		ecache = epochCache{
			atxs:       map[types.ATXID]*ATXData{},
			identities: map[types.NodeID][]types.ATXID{},
		}
	}
	if _, exists := ecache.atxs[atx]; exists {
		return
	}
	ecache.atxs[atx] = data
	atxs := ecache.identities[node]
	atxs = append(atxs, atx)
	// NOTE(dshulyak) doesn't make sense, as we don't guarantee that every node
	// will see same atxs. it actually should see atmost one and malfeasence proof if node equivocated
	sort.Slice(atxs, func(i, j int) bool {
		return bytes.Compare(atxs[i].Bytes(), atxs[j].Bytes()) == -1
	})
	ecache.identities[node] = atxs
}

func (c *Cache) SetMalicious(epoch types.EpochID, node types.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		return
	}
	for _, atx := range ecache.identities[node] {
		data := ecache.atxs[atx]
		// copy on update instead of copying on read
		updated := *data
		updated.Malicious = true
		ecache.atxs[atx] = &updated
	}
}

func (c *Cache) Get(epoch types.EpochID, atx types.ATXID) *ATXData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		return nil
	}
	data, exists := ecache.atxs[atx]
	if !exists {
		return nil
	}
	return data
}

func (c *Cache) GetByNode(epoch types.EpochID, node types.NodeID) *ATXData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		return nil
	}
	atxids, exists := ecache.identities[node]
	if !exists {
		return nil
	}
	data, exists := ecache.atxs[atxids[0]]
	if !exists {
		return nil
	}
	return data
}
