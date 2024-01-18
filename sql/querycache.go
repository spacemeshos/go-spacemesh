package sql

import (
	"slices"
	"sync"
)

type queryCacheKey struct {
	Kind string
	Key  string
}

// QueryCacheKey creates a key for QueryCache.
func QueryCacheKey(kind, key string) queryCacheKey {
	return queryCacheKey{Kind: kind, Key: key}
}

// QueryCacheSubKey denotes a cache subkey. The empty subkey refers to the main
// key. All other subkeys are cleared by UpdateSlice for the key. The subkeys
// are intended to store data derived from the query results, such as serialized
// responses.
type QueryCacheSubKey string

const (
	// When UpdateSlice method or AppendToCachedSlice function is
	// called with mainSubKey, all other subkeys for the CacheKey
	// are invalidated.
	mainSubKey QueryCacheSubKey = ""
)

type (
	// UntypedRetrieveFunc retrieves a value to be cached.
	UntypedRetrieveFunc func() (any, error)
	// SliceAppender modifies slice value stored in the cache, appending the
	// specified item to it and returns the updated slice.
	SliceAppender func(s any) any
)

// QueryCache stores results of SQL queries and data derived from these results.
// Presently, the cached entries are never removed, but eventually, it might
// become an LRU cache.
type QueryCache interface {
	// GetValue retrieves the specified key+subKey value from the cache. If
	// the entry is absent from cache, it's populated by calling retrieve func.
	// Note that the retrieve func should never cause UpdateSlice to be
	// called for this cache.
	GetValue(key queryCacheKey, subKey QueryCacheSubKey, retrieve UntypedRetrieveFunc) (any, error)
	// UpdateSlice updates the slice stored in the cache by invoking the
	// specified SliceAppender. If the entry is not cached, the method does
	// nothing.
	UpdateSlice(key queryCacheKey, update SliceAppender)
}

// RetrieveFunc retrieves a value to be stored in the cache.
type RetrieveFunc[T any] func() (T, error)

// WithCachedValue retrieves the specified value from the cache. If the entry is
// absent from the cache, it's populated by calling retrieve func. Note that the
// retrieve func should never cause UpdateSlice to be called.
func WithCachedValue[T any](db any, key queryCacheKey, retrieve func() (T, error)) (T, error) {
	return WithCachedSubKey(db, key, mainSubKey, retrieve)
}

// WithCachedValue retrieves the specified value identified by the key and
// subKey from the cache. If the entry is absent from the cache, it's populated
// by calling retrieve func. Note that the retrieve func should never cause
// UpdateSlice to be called.
func WithCachedSubKey[T any](
	db any,
	key queryCacheKey,
	subKey QueryCacheSubKey,
	retrieve func() (T, error),
) (T, error) {
	cache, ok := db.(QueryCache)
	if !ok {
		return retrieve()
	}

	v, err := cache.GetValue(key, subKey, func() (any, error) { return retrieve() })
	if err != nil {
		var r T
		return r, err
	}
	return v.(T), err
}

// AppendToCachedSlice adds a value to the slice stored in the cache by invoking
// the specified SliceAppender. If the entry is not cached, the function does
// nothing.
func AppendToCachedSlice[T any](db any, key queryCacheKey, v T) {
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
	key    queryCacheKey
	subKey QueryCacheSubKey
}

type queryCache struct {
	sync.Mutex
	updateMtx sync.RWMutex
	subKeyMap map[queryCacheKey][]QueryCacheSubKey
	values    map[fullCacheKey]any
}

var _ QueryCache = &queryCache{}

func (c *queryCache) get(key queryCacheKey, subKey QueryCacheSubKey) (any, bool) {
	c.Lock()
	defer c.Unlock()
	v, found := c.values[fullCacheKey{key: key, subKey: subKey}]
	return v, found
}

func (c *queryCache) set(key queryCacheKey, subKey QueryCacheSubKey, v any) {
	c.Lock()
	defer c.Unlock()
	if subKey != mainSubKey {
		sks := c.subKeyMap[key]
		if slices.Index(sks, subKey) < 0 {
			if c.subKeyMap == nil {
				c.subKeyMap = make(map[queryCacheKey][]QueryCacheSubKey)
			}
			c.subKeyMap[key] = append(sks, subKey)
		}
	}
	fk := fullCacheKey{key: key, subKey: subKey}
	if c.values == nil {
		c.values = map[fullCacheKey]any{fk: v}
	} else {
		c.values[fk] = v
	}
}

func (c *queryCache) GetValue(key queryCacheKey, subKey QueryCacheSubKey, retrieve UntypedRetrieveFunc) (any, error) {
	if c == nil {
		return retrieve()
	}
	c.updateMtx.RLock()
	defer c.updateMtx.RUnlock()
	v, found := c.get(key, subKey)
	var err error
	if !found {
		// This may seem like a race, but at worst, retrieve() will be
		// called several times when populating this cached entry.
		// That's better than locking for the duration of retrieve(),
		// which can also refer to this cache
		v, err = retrieve()
		if err == nil {
			c.set(key, subKey, v)
		}
	}
	return v, err
}

func (c *queryCache) UpdateSlice(key queryCacheKey, update SliceAppender) {
	if c == nil {
		return
	}
	// Here we lock for the call b/c we can't have conflicting updates for
	// the slice at the same time
	c.updateMtx.Lock()
	defer c.updateMtx.Unlock()
	fk := fullCacheKey{key: key, subKey: mainSubKey}
	if _, found := c.values[fk]; found {
		c.values[fk] = update(c.values[fk])
		for _, sk := range c.subKeyMap[key] {
			delete(c.values, fullCacheKey{key: key, subKey: sk})
		}
	}
}
