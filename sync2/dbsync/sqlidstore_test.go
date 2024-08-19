package dbsync

import (
	"context"
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
	verify := func(t *testing.T, ctx context.Context) {
		store := newDBBackedStore(db, fakeIDQuery, 8)
		it := store.iter(ctx, KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
		var actualIDs []KeyBytes
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

		it = store.start(ctx)
		for n := range 5 {
			require.Equal(t, actualIDs[n], itKey(t, it))
			require.NoError(t, it.Next())
		}

		require.NoError(t, store.registerHash(KeyBytes{0, 0, 0, 2, 0, 0, 0, 0}))
		require.NoError(t, store.registerHash(KeyBytes{0, 0, 0, 9, 0, 0, 0, 0}))
		actualIDs = nil
		it = store.iter(ctx, KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
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

	t.Run("no transaction", func(t *testing.T) {
		verify(t, context.Background())
	})

	t.Run("with transaction", func(t *testing.T) {
		err := WithSQLTransaction(context.Background(), db, func(ctx context.Context) error {
			verify(t, ctx)
			return nil
		})
		require.NoError(t, err)
	})
}
