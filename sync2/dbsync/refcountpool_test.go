package dbsync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRCPool(t *testing.T) {
	type foo struct{ x int }
	type fooIndex uint32
	// TODO: convert to TestRCPool
	var pool rcPool[foo, fooIndex]
	idx1 := pool.add(foo{x: 1})
	foo1 := pool.item(idx1)
	idx2 := pool.add(foo{x: 2})
	foo2 := pool.item(idx2)
	require.Equal(t, foo{x: 1}, foo1)
	require.Equal(t, foo{x: 2}, foo2)
	idx3 := pool.add(foo{x: 3})
	idx4 := pool.add(foo{x: 4})
	require.Equal(t, fooIndex(3), idx4)
	pool.ref(idx4)

	pool.release(idx4)
	// not yet released due to an extra ref
	require.Equal(t, fooIndex(4), pool.add(foo{x: 5}))

	pool.release(idx4)
	// idx4 was freed
	require.Equal(t, idx4, pool.add(foo{x: 6}))

	// free item used just once
	require.Equal(t, fooIndex(5), pool.add(foo{x: 7}))

	// form a free list containing several items
	pool.release(idx3)
	pool.release(idx2)
	pool.release(idx1)

	// the free list is LIFO
	require.Equal(t, idx1, pool.add(foo{x: 8}))
	require.Equal(t, idx2, pool.add(foo{x: 9}))
	require.Equal(t, idx3, pool.add(foo{x: 10}))

	// the free list is exhausted
	require.Equal(t, fooIndex(6), pool.add(foo{x: 11}))
}
