package types

import (
	"fmt"
	"sync"
)

// Thread safe
type DoubleCache struct {
	size       uint
	cacheA     map[Hash12]struct{}
	cacheB     map[Hash12]struct{}
	cacheMutex sync.RWMutex
}

func NewDoubleCache(size uint) *DoubleCache {
	return &DoubleCache{size, make(map[Hash12]struct{}, size), make(map[Hash12]struct{}, size), sync.RWMutex{}}
}

// Checks if a value is already in the cache, otherwise add it. Returns bool whether or not the value was found in the cache
// (true - already in cache, false - not in cache)
func (a *DoubleCache) GetOrInsert(key Hash12) bool {
	a.cacheMutex.Lock()
	defer a.cacheMutex.Unlock()
	if a.get(key) {
		return true
	}
	a.insert(key)
	return false
}

func (a *DoubleCache) Get(key Hash12) bool {
	a.cacheMutex.RLock()
	defer a.cacheMutex.RUnlock()
	return a.get(key)
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

// Thread safe
type DoubleResultCache struct {
	size   uint
	cacheA map[Hash12]bool
	cacheB map[Hash12]bool
	mutex  sync.RWMutex
}

func NewDoubleResultCache(size uint) *DoubleResultCache {
	return &DoubleResultCache{size, make(map[Hash12]bool, size), make(map[Hash12]bool, size), sync.RWMutex{}}
}

// Checks if a value is already in the cache, otherwise add it. Returns 2 bools -
// result - (only) in case the key was found (exist == true), returns the value
// exist - saying whether or not the value was found in the cache (true - already in cache, false - not in cache)
// err - inconsistent state, other outputs should be ignored
func (a *DoubleResultCache) GetOrInsert(key Hash12, val bool) (result bool, exist bool, err error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	res, ok := a.get(key)
	if ok {
		if res != val {
			// inconsistent state
			return false, false, fmt.Errorf("current val %v is different than new val %v", res, val)
		}
		return res, true, nil
	}
	a.insert(key, val)
	return false, false, nil
}

// Checks if a value is already in the cache. Returns 2 bools -
// result - (only) in case the key was found (exist == true), returns the value
// exist - saying whether or not the value was found in the cache (true - already in cache, false - not in cache)
func (a *DoubleResultCache) Get(key Hash12) (result bool, exist bool) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.get(key)
}

func (a *DoubleResultCache) get(key Hash12) (result bool, exist bool) {
	res, ok := a.cacheA[key]
	if ok {
		return true, res
	}
	_, ok = a.cacheB[key]
	if ok {
		return true, res
	}
	return false, false
}

func (a *DoubleResultCache) insert(key Hash12, val bool) {
	if uint(len(a.cacheA)) < a.size {
		a.cacheA[key] = val
		return
	}
	if uint(len(a.cacheB)) < a.size {
		a.cacheB[key] = val
		return
	}
	a.cacheA = a.cacheB
	a.cacheB = make(map[Hash12]bool, a.size)
	a.cacheB[key] = val
}
