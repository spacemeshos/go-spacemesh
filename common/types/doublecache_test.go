package types

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test_doubleCache(t *testing.T) {
	size := uint(10)
	c := NewDoubleCache(size)
	require.Len(t, c.cacheA, 0)
	require.Len(t, c.cacheB, 0)

	for i := uint(0); i < size; i++ {
		require.False(t, c.GetOrInsert(CalcMessageHash12([]byte(fmt.Sprintf("LOL%v", i)), "prot")))
	}

	require.Len(t, c.cacheA, int(size))

	c.GetOrInsert(CalcMessageHash12([]byte(fmt.Sprintf("LOL%v", size+1)), "prot"))
	require.Len(t, c.cacheA, int(size))
	require.Len(t, c.cacheB, 1)

	for i := uint(0); i < size-1; i++ {
		require.False(t, c.GetOrInsert(CalcMessageHash12([]byte(fmt.Sprintf("LOL%v", size+100+i)), "prot")))
	}

	require.Len(t, c.cacheA, int(size))
	require.Len(t, c.cacheB, int(size))

	cacheBitems := make(map[Hash12]struct{}, len(c.cacheB))
	for item := range c.cacheB {
		cacheBitems[item] = struct{}{}
	}

	require.False(t, c.GetOrInsert(CalcMessageHash12([]byte(fmt.Sprintf("LOL%v", size+1337)), "prot")))
	// this should prune cache a which is the oldest and keep cache b items

	require.Len(t, c.cacheB, 1)
	require.Len(t, c.cacheA, int(size))

	for item := range cacheBitems {
		_, ok := c.cacheA[item]
		require.True(t, ok)
	}

	require.True(t, c.GetOrInsert(CalcMessageHash12([]byte(fmt.Sprintf("LOL%v", size+1337)), "prot")))
	require.False(t, c.GetOrInsert(CalcMessageHash12([]byte(fmt.Sprintf("LOL%v", 0)), "prot"))) // already pruned
}
