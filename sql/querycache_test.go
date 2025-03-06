package sql

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCache(t *testing.T) {
	c := &queryCache{}
	ctx := t.Context()

	_, err := WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) (int, error) {
		return 0, errors.New("error retrieving value")
	})
	require.Error(t, err)

	v, err := WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	v, err = WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) (int, error) {
		t.Fatal("unexpected call for cached value")
		return 0, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	v, err = WithCachedValue(ctx, c, QueryCacheKey("anotherkind", "foo"), func(context.Context) (int, error) {
		return 12345, nil
	})
	require.NoError(t, err)
	require.Equal(t, 12345, v)

	v, err = WithCachedValue(ctx, c, QueryCacheKey("tst", "bar"), func(context.Context) (int, error) {
		return 4242, nil
	})
	require.NoError(t, err)
	require.Equal(t, 4242, v)
}

func TestCacheEviction(t *testing.T) {
	c := &queryCache{
		cacheSizesByKind: map[QueryCacheKind]int{
			"kind1": 10,
		},
	}
	ctx := t.Context()

	// use up all 10 items in the LRU cache
	for i := 1; i <= 10; i++ {
		k := strconv.Itoa(i)
		v, err := WithCachedValue(ctx, c, QueryCacheKey("kind1", k), func(context.Context) (int, error) {
			return i, nil
		})
		require.NoError(t, err)
		require.Equal(t, i, v)
	}

	// This should cause the oldest key to be evicted
	v, err := WithCachedValue(ctx, c, QueryCacheKey("kind1", "6"), func(context.Context) (int, error) {
		return 6, nil
	})
	require.NoError(t, err)
	require.Equal(t, 6, v)

	// ... other keys stay in place.
	for i := 2; i <= 5; i++ {
		k := strconv.Itoa(i)
		v, err := WithCachedValue(ctx, c, QueryCacheKey("kind1", k), func(context.Context) (int, error) {
			return 0, errors.New("unexpected retrieve call")
		})
		require.NoError(t, err)
		require.Equal(t, i, v)
	}

	// Cache key evicted. We're checking it after the loop b/c re-adding the keys
	// will cause more evictions
	v, err = WithCachedValue(ctx, c, QueryCacheKey("kind1", "0"), func(context.Context) (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)
}

func TestNoCache(t *testing.T) {
	ctx := t.Context()
	for _, nc := range []any{nil, struct{}{}, (*queryCache)(nil)} {
		s, err := WithCachedValue(ctx, nc, QueryCacheKey("tst", "foo"), func(context.Context) ([]string, error) {
			return []string{"abc", "def"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"abc", "def"}, s)
	}
}
