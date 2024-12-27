package sql

import (
	"context"
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
	// GetValue retrieves the specified value from the cache. If the entry is absent
	// from cache, it's populated by calling retrieve func.  Note that the retrieve
	// func should never cause UpdateSlice to be called for this cache.
	GetValue(
		ctx context.Context,
		key QueryCacheItemKey,
		retrieve UntypedRetrieveFunc,
	) (any, error)
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
	cache, ok := db.(QueryCache)
	if !ok {
		return retrieve(ctx)
	}

	v, err := cache.GetValue(
		ctx, key,
		func(ctx context.Context) (any, error) {
			return retrieve(ctx)
		})
	if err != nil {
		var r T
		return r, err
	}
	return v.(T), nil
}

type lru = simplelru.LRU[string, any]

type queryCache struct {
	sync.Mutex
	updateMtx        sync.RWMutex
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
	lruForKind, err := simplelru.NewLRU[string, any](size, func(k string, v any) {
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

func (c *queryCache) get(key QueryCacheItemKey) (any, bool) {
	c.Lock()
	defer c.Unlock()
	lru, found := c.caches[key.Kind]
	if !found {
		return nil, false
	}

	return lru.Get(key.Key)
}

func (c *queryCache) set(key QueryCacheItemKey, v any) {
	c.Lock()
	defer c.Unlock()
	lru := c.ensureLRU(key.Kind)
	lru.Add(key.Key, v)
}

func (c *queryCache) IsCached() bool {
	return c != nil
}

func (c *queryCache) GetValue(
	ctx context.Context,
	key QueryCacheItemKey,
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
	v, found := c.get(key)
	var err error
	if !found {
		// This may seem like a race, but at worst, retrieve() will be
		// called several times when populating this cached entry.
		// That's better than locking for the duration of retrieve(),
		// which can also refer to this cache
		v, err = retrieve(context.WithValue(ctx, inGetValueCtxKey{}, true))
		if err == nil {
			c.set(key, v)
		}
	}
	return v, err
}
