package atxsdata

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

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

func New() *Data {
	return &Data{
		malicious: map[types.NodeID]struct{}{},
		epochs:    map[types.EpochID]epochCache{},
	}
}

type Data struct {
	evicted atomic.Uint32

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

// EvictEpoch is a notification for cache to evict epochs that are not useful
// to keep in memory.
func (d *Data) EvictEpoch(evict types.EpochID) {
	d.mu.Lock()
	defer d.mu.Unlock()
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

// AddFromHeader extracts relevant fields from an ActivationTx and adds them together with nonce and malicious flag.
// Returns the ATX that was added to the store (if any) or `nil` if it wasn't.
func (d *Data) AddFromAtx(atx *types.ActivationTx, malicious bool) *ATX {
	return d.Add(
		atx.TargetEpoch(),
		atx.SmesherID,
		atx.Coinbase,
		atx.ID(),
		atx.Weight,
		atx.BaseTickHeight,
		atx.TickHeight(),
		atx.VRFNonce,
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
		ecache = epochCache{
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
	}
	return true
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

func (d *Data) Size(target types.EpochID) int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ecache, exists := d.epochs[target]
	if !exists {
		return 0
	}
	return len(ecache.index)
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

func (d *Data) IterateHighTicksInEpoch(target types.EpochID, fn func(types.ATXID) bool) {
	type candidate struct {
		id types.ATXID
		*ATX
	}
	candidates := make([]candidate, 0, d.Size(target))
	d.IterateInEpoch(target, func(id types.ATXID, atx *ATX) {
		candidates = append(candidates, candidate{id: id, ATX: atx})
	}, NotMalicious)

	slices.SortFunc(candidates, func(a, b candidate) int {
		switch {
		case a.Height < b.Height:
			return 1
		case a.Height > b.Height:
			return -1
		}
		return 0
	})

	for _, c := range candidates {
		if cont := fn(c.id); !cont {
			return
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
