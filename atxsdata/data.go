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
		epochs:    map[types.EpochID]*epochCache{},
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
	epochs    map[types.EpochID]*epochCache
}

type epochCache struct {
	nonDecreasingWeight uint64
	index               map[types.ATXID]*ATX
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
// Returns the ATX that was added to the store (if any) or `nil` if it wasn't.
func (d *Data) AddFromHeader(atx *types.ActivationTxHeader, nonce types.VRFPostIndex, malicious bool) *ATX {
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

// Add adds ATX data to the store.
// Returns whether the ATX was added to the store.
func (d *Data) AddAtx(target types.EpochID, id types.ATXID, atx *ATX) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.IsEvicted(target) {
		return false
	}
	ecache, exists := d.epochs[target]
	if !exists {
		ecache = &epochCache{
			index: map[types.ATXID]*ATX{},
		}
		d.epochs[target] = ecache
	}

	if _, exists = ecache.index[id]; exists {
		return false
	}

	atxsCounter.WithLabelValues(target.String()).Inc()

	ecache.index[id] = atx
	if atx.malicious {
		d.malicious[atx.Node] = struct{}{}
	} else {
		ecache.nonDecreasingWeight += atx.Weight
	}
	return true
}

func (d *Data) AddFromHeaderWithReplacement(
	atx *types.ActivationTxHeader,
	nonce types.VRFPostIndex,
	malicious bool,
	knownLargest *types.ATXID,
) *ATX {
	return d.AddWithReplacement(
		atx.TargetEpoch(),
		atx.ID,
		&ATX{
			Node:       atx.NodeID,
			Coinbase:   atx.Coinbase,
			Weight:     atx.GetWeight(),
			BaseHeight: atx.BaseTickHeight,
			Height:     atx.TickHeight(),
			Nonce:      nonce,
			malicious:  malicious,
		},
		knownLargest,
	)
}

func (d *Data) AddWithReplacement(target types.EpochID, id types.ATXID, atx *ATX, knownLargest *types.ATXID) *ATX {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.IsEvicted(target) {
		return nil
	}
	ecache, exists := d.epochs[target]
	if !exists {
		ecache = &epochCache{
			index: map[types.ATXID]*ATX{},
		}
		d.epochs[target] = ecache
	}

	if _, exists = ecache.index[id]; exists {
		return nil
	}

	atxsCounter.WithLabelValues(target.String()).Inc()

	ecache.index[id] = atx
	if atx.malicious {
		d.malicious[atx.Node] = struct{}{}
	}
	if knownLargest == nil {
		ecache.nonDecreasingWeight += atx.Weight
	} else {
		largest := ecache.index[*knownLargest]
		if largest.Weight < atx.Weight {
			ecache.nonDecreasingWeight += atx.Weight - largest.Weight
		}
	}
	return atx
}

// Add adds ATX data to the store.
// Returns the ATX that was added to the store (if any) or `nil` if it wasn't.
func (d *Data) Add(
	epoch types.EpochID,
	node types.NodeID,
	coinbase types.Address,
	atxid types.ATXID,
	weight, baseHeight, height uint64,
	nonce types.VRFPostIndex,
	malicious bool,
) *ATX {
	atx := &ATX{
		Node:       node,
		Coinbase:   coinbase,
		Weight:     weight,
		BaseHeight: baseHeight,
		Height:     height,
		Nonce:      nonce,
		malicious:  malicious,
	}
	if d.AddAtx(epoch, atxid, atx) {
		return atx
	}
	return nil
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

type lockGuard struct{}

// AtxFilter is a function that filters atxs.
// The `lockGuard` prevents using the filter functions outside of the allowed context
// to prevent data races.
type AtxFilter func(*ATX, lockGuard) bool

func NotMalicious(data *ATX, _ lockGuard) bool {
	return !data.malicious
}

// IterateInEpoch calls `fn` for every ATX in epoch.
// If filters are provided, only atxs that pass all filters are returned.
// SAFETY: The returned pointer MUST NOT be modified.
func (d *Data) IterateInEpoch(epoch types.EpochID, fn func(types.ATXID, *ATX), filters ...AtxFilter) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[epoch]
	if !exists {
		return
	}
	for id, atx := range ecache.index {
		if _, exists := d.malicious[atx.Node]; exists {
			atx.malicious = true
		}
		ok := true
		for _, filter := range filters {
			ok = ok && filter(atx, lockGuard{})
		}
		if ok {
			fn(id, atx)
		}
	}
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

// NonDecreasingWeight is used to validate that ballots don't artificially decrease number of eligibilities.
// it has to be non decreasing as if node equivocates other nodes on the network can learn equivocators in different
// order.
func (d *Data) NonDecreasingWeight(target types.EpochID) uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[target]
	if !exists {
		return 0
	}
	return ecache.nonDecreasingWeight
}
