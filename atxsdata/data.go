package atxsdata

import (
	"sync"
	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// minCapacity is set to 2 epochs because we are using data from current epoch for tortoise
// and at the start of current epoch we still need data from previous epoch for hare oracle.
const minCapacity = 2

// SAFETY: all exported fields are read-only and are safe to read concurrently.
// Thanks to the fact that ATX is immutable, it is safe to return a pointer to it.
type ATX struct {
	Node               types.NodeID
	Coinbase           types.Address
	Weight             uint64
	BaseHeight, Height uint64
	Nonce              types.VRFPostIndex
	// unexported to avoid accidental unsynchronized access
	// (this field is mutated by the Data under a lock and
	// might only be safely read under the same lock)
	malicious bool
}

type Opt func(*Data)

// WithCapacity sets the number of epochs from the latest applied
// that cache will maintain in memory.
func WithCapacity(capacity types.EpochID) Opt {
	return func(cache *Data) {
		cache.capacity = max(minCapacity, capacity)
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

// AddFromVerified extracts relevant fields from verified atx and adds them together with nonce and malicious flag.
// Returns true if the atx was added to the store.
func (d *Data) AddFromHeader(atx *types.ActivationTxHeader, nonce types.VRFPostIndex, malicious bool) bool {
	return d.Add(
		atx.TargetEpoch(),
		atx.NodeID,
		atx.Coinbase,
		atx.ID,
		atx.GetWeight(),
		atx.BaseTickHeight,
		atx.TickHeight(),
		nonce,
		malicious,
	)
}

// Add adds atx data to the store.
// Returns true if the atx was added to the store.
func (d *Data) Add(
	epoch types.EpochID,
	node types.NodeID,
	coinbase types.Address,
	atx types.ATXID,
	weight, baseHeight, height uint64,
	nonce types.VRFPostIndex,
	malicious bool,
) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.IsEvicted(epoch) {
		return false
	}
	ecache, exists := d.epochs[epoch]
	if !exists {
		ecache = epochCache{
			index: map[types.ATXID]*ATX{},
		}
		d.epochs[epoch] = ecache
	}

	if _, exists = ecache.index[atx]; exists {
		return false
	}

	atxsCounter.WithLabelValues(epoch.String()).Inc()
	data := &ATX{
		Node:       node,
		Coinbase:   coinbase,
		Weight:     weight,
		BaseHeight: baseHeight,
		Height:     height,
		Nonce:      nonce,
		malicious:  malicious,
	}
	ecache.index[atx] = data
	if malicious {
		d.malicious[node] = struct{}{}
	}
	return true
}

func (d *Data) IsMalicious(node types.NodeID) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, exists := d.malicious[node]
	return exists
}

func (d *Data) SetMalicious(node types.NodeID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.malicious[node] = struct{}{}
}

// Get returns atx data.
// SAFETY: The returned pointer MUST NOT be modified.
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
	data.malicious = exists
	return data
}

func NotMalicious(data *ATX) bool {
	return !data.malicious
}

// GetInEpoch returns all atxs in the epoch.
// If filters are provided, only atxs that pass all filters are returned.
// SAFETY: The returned pointers MUST NOT be modified.
func (d *Data) GetInEpoch(epoch types.EpochID, filters ...func(*ATX) bool) []*ATX {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[epoch]
	if !exists {
		return nil
	}
	atxs := make([]*ATX, 0, len(ecache.index))
	for _, atx := range ecache.index {
		if _, exists := d.malicious[atx.Node]; exists {
			atx.malicious = true
		}
		ok := true
		for _, filter := range filters {
			ok = ok && filter(atx)
		}
		if ok {
			atxs = append(atxs, atx)
		}
	}
	return atxs
}

func (d *Data) MissingInEpoch(epoch types.EpochID, atxs []types.ATXID) []types.ATXID {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[epoch]
	if !exists {
		return atxs
	}
	var missing []types.ATXID
	for _, id := range atxs {
		if _, exists := ecache.index[id]; !exists {
			missing = append(missing, id)
		}
	}
	return missing
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
