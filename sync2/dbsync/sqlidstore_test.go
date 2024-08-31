package dbsync

import (
	"context"
	"testing"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/stretchr/testify/require"
)

func TestDBBackedStore(t *testing.T) {
	db := sql.InMemoryTest(t)
	_, err := db.Exec(
		"create table foo(id char(8) not null primary key, received int)",
		nil, nil)
	require.NoError(t, err)
	for _, row := range []struct {
		id KeyBytes
		ts int64
	}{
		{
			id: KeyBytes{0, 0, 0, 1, 0, 0, 0, 0},
			ts: 100,
		},
		{
			id: KeyBytes{0, 0, 0, 3, 0, 0, 0, 0},
			ts: 200,
		},
		{
			id: KeyBytes{0, 0, 0, 5, 0, 0, 0, 0},
			ts: 300,
		},
		{
			id: KeyBytes{0, 0, 0, 7, 0, 0, 0, 0},
			ts: 400,
		},
	} {
		_, err := db.Exec(
			"insert into foo (id, received) values (?, ?)",
			func(stmt *sql.Statement) {
				stmt.BindBytes(1, row.id)
				stmt.BindInt64(2, row.ts)
			}, nil)
		require.NoError(t, err)
	}
	st := SyncedTable{
		TableName:       "foo",
		IDColumn:        "id",
		TimestampColumn: "received",
	}
	sts, err := st.snapshot(db)
	require.NoError(t, err)
	verify := func(t *testing.T, ctx context.Context) {
		store := newDBBackedStore(db, sts, 8)
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

		actualIDs = nil
		it, count, err := store.iterSince(ctx, KeyBytes{0, 0, 0, 0, 0, 0, 0, 0}, 300)
		require.NoError(t, err)
		require.Equal(t, 2, count)
		for range 3 {
			actualIDs = append(actualIDs, itKey(t, it))
			require.NoError(t, it.Next())
		}
		require.Equal(t, []KeyBytes{
			{0, 0, 0, 5, 0, 0, 0, 0},
			{0, 0, 0, 7, 0, 0, 0, 0},
			{0, 0, 0, 5, 0, 0, 0, 0}, // wrapped around
		}, actualIDs)

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
