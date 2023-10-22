package atxsdata

import (
	"bytes"
	"sync"

	"github.com/google/btree"
	"go.uber.org/atomic"
	"golang.org/x/exp/slices"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type ATX struct {
	Weight             uint64
	BaseHeight, Height uint64
	Nonce              types.VRFPostIndex
	Malicious          bool
}

type Opt func(*Data)

// WithCapacity sets the number of epochs from the latest applied
// that cache will maintain in memory.
func WithCapacity(capacity types.EpochID) Opt {
	return func(cache *Data) {
		cache.capacity = capacity
	}
}

// WithCapacityFromLayers sets capacity to include all layers in the window.
func WithCapacityFromLayers(window, epochSize uint32) Opt {
	capacity := window / epochSize
	if window%epochSize != 0 {
		capacity++
	}
	return WithCapacity(types.EpochID(capacity))
}

func New(opts ...Opt) *Data {
	cache := &Data{
		capacity: 2,
		epochs:   map[types.EpochID]epochCache{},
	}
	for _, opt := range opts {
		opt(cache)
	}
	return cache
}

type Data struct {
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
	data *ATX
}

type epochCache struct {
	index *btree.BTreeG[*stored]
}

func (d *Data) Evicted() types.EpochID {
	return types.EpochID(d.evicted.Load())
}

func (d *Data) IsEvicted(epoch types.EpochID) bool {
	return d.evicted.Load() >= epoch.Uint32()
}

// OnEpoch is a notification for cache to evict epochs that are not useful
// to keep in memory.
func (d *Data) OnEpoch(applied types.EpochID) {
	if applied < d.capacity {
		return
	}
	evict := applied - d.capacity
	if d.IsEvicted(evict) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.evicted.Load() < evict.Uint32() {
		d.evicted.Store(evict.Uint32())
	}
	for epoch := range d.epochs {
		if epoch <= evict {
			delete(d.epochs, epoch)
			atxsCounter.DeleteLabelValues(epoch.String())
		}
	}
}

func (d *Data) Add(epoch types.EpochID, node types.NodeID, atx types.ATXID, data *ATX) {
	if d.IsEvicted(epoch) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ecache, exists := d.epochs[epoch]
	if !exists {
		ecache = epochCache{
			index: btree.NewG(64, func(left, right *stored) bool {
				nodecmp := bytes.Compare(left.node[:], right.node[:])
				if nodecmp == 0 {
					return bytes.Compare(left.atx[:], right.atx[:]) == -1
				}
				return nodecmp == -1
			}),
		}
		d.epochs[epoch] = ecache
	}
	if _, exists := ecache.index.ReplaceOrInsert(&stored{node: node, atx: atx, data: data}); !exists {
		atxsCounter.WithLabelValues(epoch.String()).Inc()
	}

}

func (d *Data) SetMalicious(node types.NodeID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ecache := range d.epochs {
		ecache.index.AscendGreaterOrEqual(&stored{node: node}, func(s *stored) bool {
			if node != s.node {
				return false
			}
			update := *s.data
			update.Malicious = true
			s.data = &update
			return true
		})
	}
}

// Get returns atx data.
func (d *Data) Get(epoch types.EpochID, node types.NodeID, atx types.ATXID) *ATX {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[epoch]
	if !exists {
		return nil
	}
	data, exists := ecache.index.Get(&stored{node: node, atx: atx})
	if !exists {
		return nil
	}
	return data.data
}

// GetByNode returns atx data of the first atx in lexicographic order.
func (d *Data) GetByNode(epoch types.EpochID, node types.NodeID) *ATX {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[epoch]
	if !exists {
		return nil
	}
	var data *ATX
	ecache.index.AscendGreaterOrEqual(&stored{node: node}, func(s *stored) bool {
		if s.node != node {
			return false // reachable only if node is not stored
		}
		data = s.data
		return false // get the first one
	})
	return data
}

// WeightForSet computes total weight of atxs in the set and returned array with
// atxs in the set that weren't used.
//
// set is expected to be sorted.
func (d *Data) WeightForSet(epoch types.EpochID, set []types.ATXID) (uint64, []bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[epoch]

	// TODO(dshulyak) bitfield is a perfect fit here
	used := make([]bool, len(set))
	if !exists {
		return 0, used
	}
	var weight uint64
	ecache.index.Ascend(func(s *stored) bool {
		if i, exists := slices.BinarySearchFunc(set, s.atx, func(left, right types.ATXID) int {
			return bytes.Compare(left[:], right[:])
		}); exists {
			weight += s.data.Weight
			used[i] = true
		}
		return true
	})
	return weight, used
}
