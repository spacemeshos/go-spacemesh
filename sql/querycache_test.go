package sql

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCache(t *testing.T) {
	c := &queryCache{}

	_, err := WithCachedValue(c, QueryCacheKey("tst", "foo"), func() (int, error) {
		return 0, errors.New("error retrieving value")
	})
	require.Error(t, err)

	v, err := WithCachedValue(c, QueryCacheKey("tst", "foo"), func() (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	s, err := WithCachedSubKey(c, QueryCacheKey("tst", "foo"), "sk1", func() (string, error) {
		return "abc", nil
	})
	require.NoError(t, err)
	require.Equal(t, "abc", s)

	v, err = WithCachedValue(c, QueryCacheKey("tst", "foo"), func() (int, error) {
		t.Fatal("unexpected call for cached value")
		return 0, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	s, err = WithCachedSubKey(c, QueryCacheKey("tst", "foo"), "sk1", func() (string, error) {
		t.Fatal("unexpected call for cached value")
		return "", nil
	})
	require.NoError(t, err)
	require.Equal(t, "abc", s)

	v, err = WithCachedValue(c, QueryCacheKey("tst", "bar"), func() (int, error) {
		return 4242, nil
	})
	require.NoError(t, err)
	require.Equal(t, 4242, v)
}

func TestCacheSlice(t *testing.T) {
	c := &queryCache{}

	// ignored as no cached value yet
	AppendToCachedSlice(c, QueryCacheKey("tst", "foo"), "def")

	s, err := WithCachedValue(c, QueryCacheKey("tst", "foo"), func() ([]string, error) {
		return []string{"abc", "def"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def"}, s)

	s, err = WithCachedValue(c, QueryCacheKey("tst", "foo"), func() ([]string, error) {
		t.Fatal("unexpected call for cached value")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def"}, s)

	v, err := WithCachedSubKey(c, QueryCacheKey("tst", "foo"), "sk1", func() (int, error) {
		return 42, nil
	})
	require.NoError(t, err)
	require.Equal(t, 42, v)

	AppendToCachedSlice(c, QueryCacheKey("tst", "foo"), "qqq")
	s, err = WithCachedValue(c, QueryCacheKey("tst", "foo"), func() ([]string, error) {
		t.Fatal("unexpected call for cached value")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"abc", "def", "qqq"}, s)

	// invalidated
	v, err = WithCachedSubKey(c, QueryCacheKey("tst", "foo"), "sk1", func() (int, error) {
		return 4242, nil
	})
	require.NoError(t, err)
	require.Equal(t, 4242, v)
}

func TestNoCache(t *testing.T) {
	for _, nc := range []any{nil, struct{}{}, (*queryCache)(nil)} {
		s, err := WithCachedValue(nc, QueryCacheKey("tst", "foo"), func() ([]string, error) {
			return []string{"abc", "def"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"abc", "def"}, s)

		AppendToCachedSlice(nc, QueryCacheKey("tst", "foo"), "qqq")
		s, err = WithCachedValue(nc, QueryCacheKey("tst", "foo"), func() ([]string, error) {
			return []string{"abc", "def", "ghi"}, nil
		})
		require.NoError(t, err)
		require.Equal(t, []string{"abc", "def", "ghi"}, s)

		v, err := WithCachedSubKey(nc, QueryCacheKey("tst", "foo"), "sk1", func() (int, error) {
			return 4242, nil
		})
		require.NoError(t, err)
		require.Equal(t, 4242, v)

		v, err = WithCachedSubKey(nc, QueryCacheKey("tst", "foo"), "sk1", func() (int, error) {
			return 4243, nil
		})
		require.NoError(t, err)
		require.Equal(t, 4243, v)
	}
}
