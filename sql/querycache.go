package sql

import (
	"context"
	"slices"
	"sync"

	"github.com/hashicorp/golang-lru/v2/simplelru"
)

const defaultLRUCacheSize = 100

type (
	QueryCacheKind   string
	inGetValueCtxKey struct{}
)

var NullQueryCache QueryCache = (*queryCache)(nil)

type QueryCacheItemKey struct {
	Kind QueryCacheKind
	Key  string
}

// QueryCacheKey creates a key for QueryCache.
func QueryCacheKey(kind QueryCacheKind, key string) QueryCacheItemKey {
	return QueryCacheItemKey{Kind: kind, Key: key}
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
	UntypedRetrieveFunc func(ctx context.Context) (any, error)
	// SliceAppender modifies slice value stored in the cache, appending the
	// specified item to it and returns the updated slice.
	SliceAppender func(s any) any
)

// QueryCache stores results of SQL queries and data derived from these results.
// Presently, the cached entries are never removed, but eventually, it might
// become an LRU cache.
type QueryCache interface {
	// IsCached returns true if the requests are being cached.
	IsCached() bool
	// GetValue retrieves the specified key+subKey value from the cache. If
	// the entry is absent from cache, it's populated by calling retrieve func.
	// Note that the retrieve func should never cause UpdateSlice to be
	// called for this cache.
	GetValue(
		ctx context.Context,
		key QueryCacheItemKey,
		subKey QueryCacheSubKey,
		retrieve UntypedRetrieveFunc,
	) (any, error)
	// UpdateSlice updates the slice stored in the cache by invoking the
	// specified SliceAppender. If the entry is not cached, the method does
	// nothing.
	UpdateSlice(key QueryCacheItemKey, update SliceAppender)
	// ClearCache empties the cache.
	ClearCache()
}

// RetrieveFunc retrieves a value to be stored in the cache.
type RetrieveFunc[T any] func() (T, error)

// IsCached returns true if the database is cached.
func IsCached(db any) bool {
	cache, ok := db.(QueryCache)
	return ok && cache.IsCached()
}

// WithCachedValue retrieves the specified value from the cache. If the entry is
// absent from the cache, it's populated by calling retrieve func. Note that the
// retrieve func should never cause UpdateSlice to be called.
func WithCachedValue[T any](
	ctx context.Context,
	db any,
	key QueryCacheItemKey,
	retrieve func(ctx context.Context) (T, error),
) (T, error) {
	return WithCachedSubKey(ctx, db, key, mainSubKey, retrieve)
}

// WithCachedValue retrieves the specified value identified by the key and
// subKey from the cache. If the entry is absent from the cache, it's populated
// by calling retrieve func. Note that the retrieve func should never cause
// UpdateSlice to be called.
func WithCachedSubKey[T any](
	ctx context.Context,
	db any,
	key QueryCacheItemKey,
	subKey QueryCacheSubKey,
	retrieve func(ctx context.Context) (T, error),
) (T, error) {
	cache, ok := db.(QueryCache)
	if !ok {
		return retrieve(ctx)
	}

	v, err := cache.GetValue(
		ctx, key, subKey,
		func(ctx context.Context) (any, error) {
			return retrieve(ctx)
		})
	if err != nil {
		var r T
		return r, err
	}
	return v.(T), nil
}

// AppendToCachedSlice adds a value to the slice stored in the cache by invoking
// the specified SliceAppender. If the entry is not cached, the function does
// nothing.
func AppendToCachedSlice[T any](db any, key QueryCacheItemKey, v T) {
	if cache, ok := db.(QueryCache); ok {
		cache.UpdateSlice(key, func(s any) any {
			if s == nil {
				return []T{v}
			}
			return append(s.([]T), v)
		})
	}
}

type lruCacheKey struct {
	key    string
	subKey QueryCacheSubKey
}

type lru = simplelru.LRU[lruCacheKey, any]

type queryCache struct {
	sync.Mutex
	updateMtx        sync.RWMutex
	subKeyMap        map[QueryCacheItemKey][]QueryCacheSubKey
	cacheSizesByKind map[QueryCacheKind]int
	caches           map[QueryCacheKind]*lru
}

var _ QueryCache = &queryCache{}

func (c *queryCache) ensureLRU(kind QueryCacheKind) *lru {
	if lruForKind, found := c.caches[kind]; found {
		return lruForKind
	}
	size, found := c.cacheSizesByKind[kind]
	if !found || size <= 0 {
		size = defaultLRUCacheSize
	}
	lruForKind, err := simplelru.NewLRU[lruCacheKey, any](size, func(k lruCacheKey, v any) {
		if k.subKey == mainSubKey {
			c.clearSubKeys(QueryCacheItemKey{Kind: kind, Key: k.key})
		}
	})
	if err != nil {
		panic("NewLRU failed: " + err.Error())
	}
	if c.caches == nil {
		c.caches = make(map[QueryCacheKind]*lru)
	}
	c.caches[kind] = lruForKind
	return lruForKind
}

func (c *queryCache) clearSubKeys(key QueryCacheItemKey) {
	lru, found := c.caches[key.Kind]
	if !found {
		return
	}
	for _, sk := range c.subKeyMap[key] {
		lru.Remove(lruCacheKey{
			key:    key.Key,
			subKey: sk,
		})
	}
}

func (c *queryCache) get(key QueryCacheItemKey, subKey QueryCacheSubKey) (any, bool) {
	c.Lock()
	defer c.Unlock()
	lru, found := c.caches[key.Kind]
	if !found {
		return nil, false
	}

	return lru.Get(lruCacheKey{
		key:    key.Key,
		subKey: subKey,
	})
}

func (c *queryCache) set(key QueryCacheItemKey, subKey QueryCacheSubKey, v any) {
	c.Lock()
	defer c.Unlock()
	if subKey != mainSubKey {
		sks := c.subKeyMap[key]
		if slices.Index(sks, subKey) < 0 {
			if c.subKeyMap == nil {
				c.subKeyMap = make(map[QueryCacheItemKey][]QueryCacheSubKey)
			}
			c.subKeyMap[key] = append(sks, subKey)
		}
	}
	lru := c.ensureLRU(key.Kind)
	lru.Add(lruCacheKey{key: key.Key, subKey: subKey}, v)
}

func (c *queryCache) IsCached() bool {
	return c != nil
}

func (c *queryCache) GetValue(
	ctx context.Context,
	key QueryCacheItemKey,
	subKey QueryCacheSubKey,
	retrieve UntypedRetrieveFunc,
) (any, error) {
	if c == nil {
		return retrieve(ctx)
	}
	// Avoid recursive locking from within retrieve()
	if ctx.Value(inGetValueCtxKey{}) == nil {
		c.updateMtx.RLock()
		defer c.updateMtx.RUnlock()
	}
	v, found := c.get(key, subKey)
	var err error
	if !found {
		// This may seem like a race, but at worst, retrieve() will be
		// called several times when populating this cached entry.
		// That's better than locking for the duration of retrieve(),
		// which can also refer to this cache
		v, err = retrieve(context.WithValue(ctx, inGetValueCtxKey{}, true))
		if err == nil {
			c.set(key, subKey, v)
		}
	}
	return v, err
}

func (c *queryCache) UpdateSlice(key QueryCacheItemKey, update SliceAppender) {
	if c == nil {
		return
	}

	// Here we lock for the call b/c we can't have conflicting updates for
	// the slice at the same time
	c.updateMtx.Lock()
	c.Lock()
	defer func() {
		c.Unlock()
		c.updateMtx.Unlock()
	}()
	lru, found := c.caches[key.Kind]
	if !found {
		return
	}

	k := lruCacheKey{key: key.Key, subKey: mainSubKey}
	if v, found := lru.Get(k); found {
		lru.Add(k, update(v))
		c.clearSubKeys(key)
	}
}

func (c *queryCache) ClearCache() {
	if c == nil {
		return
	}
	c.updateMtx.Lock()
	c.Lock()
	defer func() {
		c.Unlock()
		c.updateMtx.Unlock()
	}()
	// No need to clear c.subKeyMap as it's only used to keep track of possible subkeys for each key
	c.caches = nil
}
