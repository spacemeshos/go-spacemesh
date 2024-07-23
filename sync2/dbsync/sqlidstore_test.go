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
	store := newDBBackedStore(db, fakeIDQuery, 8, 24)
	var actualIDs []KeyBytes
	it, err := store.iter(KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
	require.NoError(t, err)
	for range 5 {
		actualIDs = append(actualIDs, it.Key().(KeyBytes))
		require.NoError(t, it.Next())
	}
	require.Equal(t, []KeyBytes{
		{0, 0, 0, 1, 0, 0, 0, 0},
		{0, 0, 0, 3, 0, 0, 0, 0},
		{0, 0, 0, 5, 0, 0, 0, 0},
		{0, 0, 0, 7, 0, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 0, 0}, // wrapped around
	}, actualIDs)

	require.NoError(t, store.registerHash(KeyBytes{0, 0, 0, 2, 0, 0, 0, 0}))
	require.NoError(t, store.registerHash(KeyBytes{0, 0, 0, 9, 0, 0, 0, 0}))
	actualIDs = nil
	it, err = store.iter(KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
	require.NoError(t, err)
	for range 6 {
		actualIDs = append(actualIDs, it.Key().(KeyBytes))
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
