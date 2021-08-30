package types

import (
	"sync"
)

// DoubleCache is a structure for storing which keys have been encountered before. It's initialized with a size and it
// stores between size and 2*size keys. Every time the cache size reaches 2*size and a new value is added, the oldest
// size keys are discarded and the size drops back to size.
// DoubleCache is thread safe.
type DoubleCache struct {
	size       uint
	cacheA     map[Hash12]struct{}
	cacheB     map[Hash12]struct{}
	cacheMutex sync.RWMutex
}

// NewDoubleCache returns a new DoubleCache.
func NewDoubleCache(size uint) *DoubleCache {
	return &DoubleCache{size: size, cacheA: make(map[Hash12]struct{}, size), cacheB: make(map[Hash12]struct{}, size)}
}

// GetOrInsert checks if a value is already in the cache, otherwise it adds it. Returns bool whether or not the value
// was found in the cache (true - already in cache, false - wasn't in cache before this was called).
func (a *DoubleCache) GetOrInsert(key Hash12) bool {
	a.cacheMutex.Lock()
	defer a.cacheMutex.Unlock()
	if a.get(key) {
		return true
	}
	a.insert(key)
	return false
}

func (a *DoubleCache) get(key Hash12) bool {
	_, ok := a.cacheA[key]
	if ok {
		return true
	}
	_, ok = a.cacheB[key]
	if ok {
		return true
	}
	return false
}

func (a *DoubleCache) insert(key Hash12) {
	if uint(len(a.cacheA)) < a.size {
		a.cacheA[key] = struct{}{}
		return
	}
	if uint(len(a.cacheB)) < a.size {
		a.cacheB[key] = struct{}{}
		return
	}
	a.cacheA = a.cacheB
	a.cacheB = make(map[Hash12]struct{}, a.size)
	a.cacheB[key] = struct{}{}
}
