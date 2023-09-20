package cache

import (
	"bytes"
	"sort"
	"sync"

	"go.uber.org/atomic"

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
	evicted atomic.Uint32

	// number of epochs to keep
	// capacity is enforced by the cache itself
	capacity types.EpochID

	mu     sync.RWMutex
	epochs map[types.EpochID]epochCache
}

type epochCache struct {
	atxs       map[types.ATXID]*ATXData
	identities map[types.NodeID][]types.ATXID
}

func (c *Cache) TargetCapacity() types.EpochID {
	return c.capacity
}

func (c *Cache) IsEvicted(epoch types.EpochID) bool {
	return c.evicted.Load() >= epoch.Uint32()
}

// Evict supposed to be called when application knows that keeping epoch in memory is not useful.
//
// Good heuristic is when layer from higher epoch was applied, at that point it is unlikely
// that we will see a lot of data from past epochs.
func (c *Cache) Evict(evict types.EpochID) {
	if c.IsEvicted(evict) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.evicted.Load() < evict.Uint32() {
		c.evicted.Store(evict.Uint32())
	}
	for epoch := range c.epochs {
		if epoch <= evict {
			delete(c.epochs, epoch)
		}
	}
}

func (c *Cache) Add(epoch types.EpochID, node types.NodeID, atx types.ATXID, data *ATXData) {
	if c.IsEvicted(epoch) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		ecache = epochCache{
			atxs:       map[types.ATXID]*ATXData{},
			identities: map[types.NodeID][]types.ATXID{},
		}
		c.epochs[epoch] = ecache
	}
	if _, exists := ecache.atxs[atx]; exists {
		return
	}
	ecache.atxs[atx] = data
	atxs := ecache.identities[node]
	atxs = append(atxs, atx)
	// NOTE(dshulyak) doesn't make sense, as we don't guarantee that every node
	// will see same atxs. it actually should see atmost one and malfeasence proof if node equivocated.
	// find out if we have use case in consensus.
	sort.Slice(atxs, func(i, j int) bool {
		return bytes.Compare(atxs[i].Bytes(), atxs[j].Bytes()) == -1
	})
	ecache.identities[node] = atxs
}

func (c *Cache) SetMalicious(node types.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ecache := range c.epochs {
		for _, atx := range ecache.identities[node] {
			data := ecache.atxs[atx]
			// copy on update instead of copying on read
			updated := *data
			updated.Malicious = true
			ecache.atxs[atx] = &updated
		}
	}
}

// Get returns atx data.
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

// GetByNode returns atx data of the first atx in lexicographic order.
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
	for _, atxid := range atxids {
		data, exists := ecache.atxs[atxid]
		if exists {
			return data
		}
	}
	return nil
}

// NodeHasAtx returns true if atx was registered with a given node id.
func (c *Cache) NodeHasAtx(epoch types.EpochID, node types.NodeID, atx types.ATXID) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		return false
	}
	atxids, exists := ecache.identities[node]
	if !exists {
		return false
	}
	for i := range atxids {
		if atxids[i] == atx {
			return true
		}
	}
	return false
}
