package fptree

import (
	"strconv"
	"sync/atomic"
)

// freeBit is a bit that indicates that an entry is free.
const freeBit = 1 << 31

// freeListMask is a mask that extracts the free list index from a refCount.
const freeListMask = freeBit - 1

// poolEntry is an entry in the rcPool.
type poolEntry[T any, I ~uint32] struct {
	refCount uint32
	content  T
}

// rcPool is a reference-counted pool of items.
// The zero value is a valid, empty rcPool.
// Unlike sync.Pool, rcPool does not shrink, but uint32 indices can be used
// to reference items instead of larger 64-bit pointers, and the items
// can be shared between.
type rcPool[T any, I ~uint32] struct {
	entries []poolEntry[T, I]
	// freeList is 1-based so that rcPool doesn't need a constructor
	freeList   uint32
	allocCount atomic.Int64
}

// init pre-allocates the rcPool with n items.
func (rc *rcPool[T, I]) init(n int) {
	rc.entries = make([]poolEntry[T, I], 0, n)
	rc.freeList = 0
	rc.allocCount.Store(0)
}

// count returns the number of items in the rcPool.
func (rc *rcPool[T, I]) count() int {
	return int(rc.allocCount.Load())
}

// item returns the item at the given index.
func (rc *rcPool[T, I]) item(idx I) T {
	return rc.entry(idx).content
}

// entry returns the pool entry at the given index.
func (rc *rcPool[T, I]) entry(idx I) *poolEntry[T, I] {
	entry := &rc.entries[idx]
	if entry.refCount&freeBit != 0 {
		panic("BUG: referencing a free nodePool entry " + strconv.Itoa(int(idx)))
	}
	return entry
}

// replace replaces the item at the given index.
func (rc *rcPool[T, I]) replace(idx I, item T) {
	entry := &rc.entries[idx]
	if entry.refCount&freeBit != 0 {
		panic("BUG: replace of a free rcPool[T, I] entry")
	}
	if entry.refCount != 1 {
		panic("BUG: bad rcPool[T, I] entry refcount for replace")
	}
	entry.content = item
}

// add adds an item to the rcPool and returns its index.
func (rc *rcPool[T, I]) add(item T) I {
	var idx I
	if rc.freeList != 0 {
		idx = I(rc.freeList - 1)
		rc.freeList = rc.entries[idx].refCount & freeListMask
		if rc.freeList > uint32(len(rc.entries)) {
			panic("BUG: bad freeList linkage")
		}
		rc.entries[idx].refCount = 1
	} else {
		idx = I(len(rc.entries))
		rc.entries = append(rc.entries, poolEntry[T, I]{refCount: 1})
	}
	rc.entries[idx].content = item
	rc.allocCount.Add(1)
	return idx
}

// release releases the item at the given index.
func (rc *rcPool[T, I]) release(idx I) bool {
	entry := &rc.entries[idx]
	if entry.refCount&freeBit != 0 {
		panic("BUG: release of a free rcPool[T, I] entry")
	}
	if entry.refCount <= 0 {
		panic("BUG: bad rcPool[T, I] entry refcount")
	}
	entry.refCount--
	if entry.refCount == 0 {
		if rc.freeList > uint32(len(rc.entries)) {
			panic("BUG: bad freeList")
		}
		entry.refCount = rc.freeList | freeBit
		rc.freeList = uint32(idx + 1)
		rc.allocCount.Add(-1)
		return true
	}

	return false
}

// ref adds a reference to the item at the given index.
func (rc *rcPool[T, I]) ref(idx I) {
	rc.entries[idx].refCount++
}

// refCount returns the reference count for the item at the given index.
func (rc *rcPool[T, I]) refCount(idx I) uint32 {
	return rc.entries[idx].refCount
}
