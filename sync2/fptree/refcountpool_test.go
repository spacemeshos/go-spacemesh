package fptree

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRCPool(t *testing.T) {
	type foo struct {
		//nolint:unused
		x int
	}
	type fooIndex uint32

	var pool rcPool[foo, fooIndex]
	idx1 := pool.add(foo{x: 1})
	foo1 := pool.item(idx1)
	require.Equal(t, 1, pool.count())
	idx2 := pool.add(foo{x: 2})
	foo2 := pool.item(idx2)
	require.Equal(t, 2, pool.count())
	require.Equal(t, foo{x: 1}, foo1)
	require.Equal(t, foo{x: 2}, foo2)
	idx3 := pool.add(foo{x: 3})
	idx4 := pool.add(foo{x: 4})
	require.Equal(t, fooIndex(3), idx4)
	pool.ref(idx4)
	require.Equal(t, 4, pool.count())

	require.False(t, pool.release(idx4))
	// not yet released due to an extra ref
	require.Equal(t, fooIndex(4), pool.add(foo{x: 5}))
	require.Equal(t, 5, pool.count())

	require.True(t, pool.release(idx4))
	// idx4 was freed
	require.Equal(t, idx4, pool.add(foo{x: 6}))
	require.Equal(t, 5, pool.count())

	// free item used just once
	require.Equal(t, fooIndex(5), pool.add(foo{x: 7}))
	require.Equal(t, 6, pool.count())

	// form a free list containing several items
	require.True(t, pool.release(idx3))
	require.True(t, pool.release(idx2))
	require.True(t, pool.release(idx1))
	require.Equal(t, 3, pool.count())

	// the free list is LIFO
	require.Equal(t, idx1, pool.add(foo{x: 8}))
	require.Equal(t, idx2, pool.add(foo{x: 9}))
	require.Equal(t, idx3, pool.add(foo{x: 10}))
	require.Equal(t, 6, pool.count())

	// the free list is exhausted
	idx5 := pool.add(foo{x: 11})
	require.Equal(t, fooIndex(6), idx5)
	require.Equal(t, 7, pool.count())

	// replace the item
	pool.replace(idx5, foo{x: 12})
	require.Equal(t, foo{x: 12}, pool.item(idx5))

	// // don't replace an item with multiple refs
	// pool.ref(idx5)
	// idx6, replaced := pool.addOrReplace(idx5, foo{x: 13})
	// require.False(t, replaced)
	// require.Equal(t, fooIndex(7), idx6)
	// require.Equal(t, foo{x: 12}, pool.item(idx5))
	// require.Equal(t, foo{x: 13}, pool.item(idx6))

	// // but failing to replace the item should have still decreased its ref count
	// require.True(t, pool.release(idx5))
}
