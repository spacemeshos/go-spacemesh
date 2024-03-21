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
	ctx := context.Background()

	_, err := WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) (int, error) {
		return 0, errors.New("error retrieving value")
	})
	require.Error(t, err)

	v, err := WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	s, err := WithCachedSubKey(ctx, c, QueryCacheKey("tst", "foo"), "sk1", func(context.Context) (string, error) {
		return "abc", nil
	})
	require.NoError(t, err)
	require.Equal(t, "abc", s)

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

	s, err = WithCachedSubKey(ctx, c, QueryCacheKey("tst", "foo"), "sk1", func(context.Context) (string, error) {
		t.Fatal("unexpected call for cached value")
		return "", nil
	})
	require.NoError(t, err)
	require.Equal(t, "abc", s)

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
	ctx := context.Background()

	// use up all 10 items in the LRU cache (both key and subkey is counted)
	for i := 1; i <= 5; i++ {
		k := strconv.Itoa(i)
		v, err := WithCachedValue(ctx, c, QueryCacheKey("kind1", k), func(context.Context) (int, error) {
			return i, nil
		})
		require.NoError(t, err)
		require.Equal(t, i, v)

		s, err := WithCachedSubKey(ctx, c, QueryCacheKey("kind1", k), "sk1", func(context.Context) (int, error) {
			return i * 10, nil
		})
		require.NoError(t, err)
		require.Equal(t, i*10, s)
	}

	// This should cause the oldest key to be evicted with all of its subkeys
	v, err := WithCachedValue(ctx, c, QueryCacheKey("kind1", "6"), func(context.Context) (int, error) {
		return 6, nil
	})
	require.NoError(t, err)
	require.Equal(t, 6, v)

	// ... other keys and subkeys stay in place.
	for i := 2; i <= 5; i++ {
		k := strconv.Itoa(i)
		v, err := WithCachedValue(ctx, c, QueryCacheKey("kind1", k), func(context.Context) (int, error) {
			return 0, errors.New("unexpected retrieve call")
		})
		require.NoError(t, err)
		require.Equal(t, i, v)

		s, err := WithCachedSubKey(ctx, c, QueryCacheKey("kind1", k), "sk1", func(context.Context) (int, error) {
			return 0, errors.New("unexpected retrieve call")
		})
		require.NoError(t, err)
		require.Equal(t, i*10, s)
	}

	// Cache key evicted. We're checking it after the loop b/c re-adding the keys
	// will cause more evictions
	v, err = WithCachedValue(ctx, c, QueryCacheKey("kind1", "0"), func(context.Context) (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	// subkey evicted
	s, err := WithCachedSubKey(ctx, c, QueryCacheKey("kind1", "0"), "sk1", func(context.Context) (int, error) {
		return 4242, nil
	})
	require.NoError(t, err)
	require.Equal(t, 4242, s)
}

func TestCacheSlice(t *testing.T) {
	c := &queryCache{}
	ctx := context.Background()

	// ignored as no cached value yet
	AppendToCachedSlice(c, QueryCacheKey("tst", "foo"), "def")

	s, err := WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) ([]string, error) {
		return []string{"abc", "def"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def"}, s)

	s, err = WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) ([]string, error) {
		t.Fatal("unexpected call for cached value")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def"}, s)

	v, err := WithCachedSubKey(ctx, c, QueryCacheKey("tst", "foo"), "sk1", func(context.Context) (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	AppendToCachedSlice(c, QueryCacheKey("tst", "foo"), "qqq")
	s, err = WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) ([]string, error) {
		t.Fatal("unexpected call for cached value")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def", "qqq"}, s)

	// invalidated
	v, err = WithCachedSubKey(ctx, c, QueryCacheKey("tst", "foo"), "sk1", func(context.Context) (int, error) {
		return 4242, nil
	})
	require.NoError(t, err)
	require.Equal(t, 4242, v)

	c.ClearCache()
	s, err = WithCachedValue(ctx, c, QueryCacheKey("tst", "foo"), func(context.Context) ([]string, error) {
		return []string{"new"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"new"}, s)
}

func TestNoCache(t *testing.T) {
	ctx := context.Background()
	for _, nc := range []any{nil, struct{}{}, (*queryCache)(nil)} {
		s, err := WithCachedValue(ctx, nc, QueryCacheKey("tst", "foo"), func(context.Context) ([]string, error) {
			return []string{"abc", "def"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"abc", "def"}, s)

		AppendToCachedSlice(nc, QueryCacheKey("tst", "foo"), "qqq")
		s, err = WithCachedValue(ctx, nc, QueryCacheKey("tst", "foo"), func(context.Context) ([]string, error) {
			return []string{"abc", "def", "ghi"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"abc", "def", "ghi"}, s)

		v, err := WithCachedSubKey(ctx, nc, QueryCacheKey("tst", "foo"), "sk1", func(context.Context) (int, error) {
			return 4242, nil
		})
		require.NoError(t, err)
		require.Equal(t, 4242, v)

		v, err = WithCachedSubKey(ctx, nc, QueryCacheKey("tst", "foo"), "sk1", func(context.Context) (int, error) {
			return 4243, nil
		})
		require.NoError(t, err)
		require.Equal(t, 4243, v)
	}
}
