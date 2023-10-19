package cache

import (
	"bytes"
	"sync"

	"go.uber.org/atomic"

	"github.com/google/btree"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type ATXData struct {
	Weight             uint64
	BaseHeight, Height uint64
	Nonce              types.VRFPostIndex
	Malicious          bool
}

type Opt func(*Cache)

// WithCapacity sets the number of epochs from the latest applied
// that cache will maintain in memory.
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
	// capacity is not enforced by the cache itself
	capacity types.EpochID

	mu     sync.RWMutex
	epochs map[types.EpochID]epochCache
}

type stored struct {
	node types.NodeID
	atx  types.ATXID
	data *ATXData
}

type epochCache struct {
	index *btree.BTreeG[stored]
}

func (c *Cache) Evicted() types.EpochID {
	return types.EpochID(c.evicted.Load())
}

func (c *Cache) IsEvicted(epoch types.EpochID) bool {
	return c.evicted.Load() >= epoch.Uint32()
}

// OnEpoch is a notification for cache to evict epochs that are not useful
// to keep in memory.
func (c *Cache) OnEpoch(applied types.EpochID) {
	if applied < c.capacity {
		return
	}
	evict := applied - c.capacity
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
			atxsCounter.DeleteLabelValues(epoch.String())
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
			index: btree.NewG(64, func(left, right stored) bool {
				nodecmp := bytes.Compare(left.node[:], right.node[:])
				if nodecmp == 0 {
					return bytes.Compare(left.atx[:], right.atx[:]) == -1
				}
				return nodecmp == -1
			}),
		}
		c.epochs[epoch] = ecache
	}
	if _, exists := ecache.index.ReplaceOrInsert(stored{node: node, atx: atx, data: data}); !exists {
		atxsCounter.WithLabelValues(epoch.String()).Inc()
	}

}

func (c *Cache) SetMalicious(node types.NodeID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ecache := range c.epochs {
		ecache.index.AscendGreaterOrEqual(stored{node: node}, func(s stored) bool {
			if node != s.node {
				return false
			}
			// TODO(dshulyak) how to copy on update here?
			s.data.Malicious = true
			return true
		})
	}
}

// Get returns atx data.
func (c *Cache) Get(epoch types.EpochID, node types.NodeID, atx types.ATXID) *ATXData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		return nil
	}
	data, exists := ecache.index.Get(stored{node: node, atx: atx})
	if !exists {
		return nil
	}
	return data.data
}

// GetByNode returns atx data of the first atx in lexicographic order.
func (c *Cache) GetByNode(epoch types.EpochID, node types.NodeID) *ATXData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ecache, exists := c.epochs[epoch]
	if !exists {
		return nil
	}
	var data *ATXData
	ecache.index.AscendGreaterOrEqual(stored{node: node}, func(s stored) bool {
		if s.node != node {
			return false // reachable only if node is not stored
		}
		data = s.data
		return false // get the first one
	})
	return data
}

// NodeHasAtx returns true if atx was registered with a given node id.
func (c *Cache) NodeHasAtx(epoch types.EpochID, node types.NodeID, atx types.ATXID) bool {
	data := c.Get(epoch, node, atx)
	return data != nil
}
