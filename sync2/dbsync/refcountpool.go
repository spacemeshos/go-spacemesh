package dbsync

import (
	"sync"
	"sync/atomic"
)

const freeBit = 1 << 31
const freeListMask = freeBit - 1

type poolEntry[T any, I ~uint32] struct {
	refCount uint32
	content  T
}

type rcPool[T any, I ~uint32] struct {
	mtx     sync.Mutex
	entries []poolEntry[T, I]
	// freeList is 1-based so that rcPool doesn't need a constructor
	freeList   uint32
	allocCount atomic.Int64
}

func (rc *rcPool[T, I]) count() int {
	return int(rc.allocCount.Load())
}

func (rc *rcPool[T, I]) item(idx I) T {
	rc.mtx.Lock()
	defer rc.mtx.Unlock()
	return rc.entry(idx).content
}

func (rc *rcPool[T, I]) entry(idx I) *poolEntry[T, I] {
	entry := &rc.entries[idx]
	if entry.refCount&freeBit != 0 {
		panic("BUG: referencing a free nodePool entry")
	}
	return entry
}

func (rc *rcPool[T, I]) add(item T) I {
	rc.mtx.Lock()
	defer rc.mtx.Unlock()
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

func (rc *rcPool[T, I]) release(idx I) bool {
	rc.mtx.Lock()
	defer rc.mtx.Unlock()
	entry := &rc.entries[idx]
	if entry.refCount&freeBit != 0 {
		panic("BUG: excess release of rcPool[T, I] entry")
	}
	if entry.refCount <= 0 {
		panic("BUG: negative rcPool[T, I] entry refcount")
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

func (rc *rcPool[T, I]) ref(idx I) {
	rc.mtx.Lock()
	rc.entries[idx].refCount++
	rc.mtx.Unlock()
}

func (rc *rcPool[T, I]) refCount(idx I) uint32 {
	rc.mtx.Lock()
	defer rc.mtx.Unlock()
	return rc.entries[idx].refCount
}

// TODO: convert TestNodePool to TestRCPool
