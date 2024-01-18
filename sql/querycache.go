package sql

import (
	"slices"
	"sync"
)

type QueryCacheKey struct {
	Kind string
	Key  string
}

func MkQueryCacheKey(kind, key string) QueryCacheKey {
	return QueryCacheKey{Kind: kind, Key: key}
}

type QueryCacheSubKey string

const (
	// When UpdateSlice method or AppendToCachedSlice function is
	// called with MainSubKey, all other subkeys for the CacheKey
	// are invalidated.
	MainSubKey QueryCacheSubKey = ""
)

type (
	RetrieveFunc      func() (any, error)
	QueryCacheUpdater func(s any) any
)

type QueryCache interface {
	GetValue(key QueryCacheKey, subKey QueryCacheSubKey, retrieve RetrieveFunc) (any, error)
	UpdateSlice(key QueryCacheKey, update QueryCacheUpdater)
}

type ValueGetter[T any] func() (T, error)

func WithCachedValue[T any](db any, key QueryCacheKey, getter func() (T, error)) (T, error) {
	return WithCachedSubKey(db, key, MainSubKey, getter)
}

func WithCachedSubKey[T any](db any, key QueryCacheKey, subKey QueryCacheSubKey, getter func() (T, error)) (T, error) {
	cache, ok := db.(QueryCache)
	if !ok {
		return getter()
	}

	v, err := cache.GetValue(key, subKey, func() (any, error) { return getter() })
	if err != nil {
		var r T
		return r, err
	}
	return v.(T), err
}

func AppendToCachedSlice[T any](db any, key QueryCacheKey, v T) {
	if cache, ok := db.(QueryCache); ok {
		cache.UpdateSlice(key, func(s any) any {
			if s == nil {
				return []T{v}
			}
			return append(s.([]T), v)
		})
	}
}

type fullCacheKey struct {
	key    QueryCacheKey
	subKey QueryCacheSubKey
}

type queryCache struct {
	sync.Mutex
	subKeyMap map[QueryCacheKey][]QueryCacheSubKey
	values    map[fullCacheKey]any
}

var _ QueryCache = &queryCache{}

func (c *queryCache) ensureSubKey(key QueryCacheKey, subKey QueryCacheSubKey) {
}

func (c *queryCache) get(key QueryCacheKey, subKey QueryCacheSubKey) (any, bool) {
	c.Lock()
	defer c.Unlock()
	v, found := c.values[fullCacheKey{key: key, subKey: subKey}]
	return v, found
}

func (c *queryCache) set(key QueryCacheKey, subKey QueryCacheSubKey, v any) {
	c.Lock()
	defer c.Unlock()
	if subKey != MainSubKey {
		sks := c.subKeyMap[key]
		if slices.Index(sks, subKey) < 0 {
			if c.subKeyMap == nil {
				c.subKeyMap = make(map[QueryCacheKey][]QueryCacheSubKey)
			}
			c.subKeyMap[key] = append(sks, subKey)
		}
	}
	c.values[fullCacheKey{key: key, subKey: subKey}] = v
}

func (c *queryCache) GetValue(key QueryCacheKey, subKey QueryCacheSubKey, retrieve RetrieveFunc) (any, error) {
	if c == nil {
		return retrieve()
	}
	v, found := c.get(key, subKey)
	var err error
	if !found {
		// This may seem like a race, but at worst, retrieve() will be
		// called several times when populating this cached entry.
		// That's better than locking for the duration of retrieve(),
		// which can also refer to this cache
		v, err = retrieve()
		if err == nil {
			c.ensureSubKey(key, subKey)
			if c.values == nil {
				c.values = make(map[fullCacheKey]any)
			}
			c.set(key, subKey, v)
		}
	}
	return v, err
}

func (c *queryCache) UpdateSlice(key QueryCacheKey, update QueryCacheUpdater) {
	if c == nil {
		return
	}
	// Here we lock for the call b/c we can't have conflicting updates for
	// the slice at the same time
	c.Lock()
	defer c.Unlock()
	fk := fullCacheKey{key: key, subKey: MainSubKey}
	if _, found := c.values[fk]; found {
		c.values[fk] = update(c.values[fk])
		for _, sk := range c.subKeyMap[key] {
			delete(c.values, fullCacheKey{key: key, subKey: sk})
		}
	}
}
