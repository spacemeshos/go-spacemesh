package atxsdata

import (
	"sync"
	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type ATX struct {
	Node               types.NodeID
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
		capacity:  2,
		malicious: map[types.NodeID]struct{}{},
		epochs:    map[types.EpochID]epochCache{},
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

	mu        sync.RWMutex
	malicious map[types.NodeID]struct{}
	epochs    map[types.EpochID]epochCache
}

type epochCache struct {
	index map[types.ATXID]*ATX
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
	d.mu.Lock()
	defer d.mu.Unlock()
	if applied < d.capacity {
		return
	}
	evict := applied - d.capacity
	if d.IsEvicted(evict) {
		return
	}
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

func (d *Data) Add(
	epoch types.EpochID,
	node types.NodeID,
	atx types.ATXID,
	weight, baseHeight, height uint64,
	nonce types.VRFPostIndex,
	malicious bool,
) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.IsEvicted(epoch) {
		return
	}
	ecache, exists := d.epochs[epoch]
	if !exists {
		ecache = epochCache{
			index: map[types.ATXID]*ATX{},
		}
		d.epochs[epoch] = ecache
	}
	_, exists = ecache.index[atx]
	if !exists {
		atxsCounter.WithLabelValues(epoch.String()).Inc()
	}
	data := &ATX{
		Node:       node,
		Weight:     weight,
		BaseHeight: baseHeight,
		Height:     height,
		Nonce:      nonce,
		Malicious:  malicious,
	}
	ecache.index[atx] = data
	if data.Malicious {
		d.malicious[node] = struct{}{}
	}
}

func (d *Data) SetMalicious(node types.NodeID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.malicious[node] = struct{}{}
}

// Get returns atx data.
func (d *Data) Get(epoch types.EpochID, atx types.ATXID) *ATX {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[epoch]
	if !exists {
		return nil
	}
	data, exists := ecache.index[atx]
	if !exists {
		return nil
	}
	_, exists = d.malicious[data.Node]
	data.Malicious = exists
	return data
}

// WeightForSet computes total weight of atxs in the set and returned array with
// atxs in the set that weren't used.
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
	for i, id := range set {
		if data, exists := ecache.index[id]; exists {
			weight += data.Weight
			used[i] = true
		}
	}
	return weight, used
}
