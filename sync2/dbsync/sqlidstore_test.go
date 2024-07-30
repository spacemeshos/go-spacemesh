package dbsync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDBBackedStore(t *testing.T) {
	initialIDs := []KeyBytes{
		{0, 0, 0, 1, 0, 0, 0, 0},
		{0, 0, 0, 3, 0, 0, 0, 0},
		{0, 0, 0, 5, 0, 0, 0, 0},
		{0, 0, 0, 7, 0, 0, 0, 0},
	}
	db := populateDB(t, 8, initialIDs)
	store := newDBBackedStore(db, fakeIDQuery, 8)
	var actualIDs []KeyBytes
	it := store.iter(KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
	for range 5 {
		actualIDs = append(actualIDs, itKey(t, it))
		require.NoError(t, it.Next())
	}
	require.Equal(t, []KeyBytes{
		{0, 0, 0, 1, 0, 0, 0, 0},
		{0, 0, 0, 3, 0, 0, 0, 0},
		{0, 0, 0, 5, 0, 0, 0, 0},
		{0, 0, 0, 7, 0, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 0, 0}, // wrapped around
	}, actualIDs)

	it = store.start()
	for n := range 5 {
		require.Equal(t, actualIDs[n], itKey(t, it))
		require.NoError(t, it.Next())
	}

	require.NoError(t, store.registerHash(KeyBytes{0, 0, 0, 2, 0, 0, 0, 0}))
	require.NoError(t, store.registerHash(KeyBytes{0, 0, 0, 9, 0, 0, 0, 0}))
	actualIDs = nil
	it = store.iter(KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
	for range 6 {
		actualIDs = append(actualIDs, itKey(t, it))
		require.NoError(t, it.Next())
	}
	require.Equal(t, []KeyBytes{
		{0, 0, 0, 1, 0, 0, 0, 0},
		{0, 0, 0, 2, 0, 0, 0, 0},
		{0, 0, 0, 3, 0, 0, 0, 0},
		{0, 0, 0, 5, 0, 0, 0, 0},
		{0, 0, 0, 7, 0, 0, 0, 0},
		{0, 0, 0, 9, 0, 0, 0, 0},
	}, actualIDs)
}
