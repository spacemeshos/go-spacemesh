package dbsync

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

func TestDBBackedStore(t *testing.T) {
	db := sql.InMemoryTest(t)
	_, err := db.Exec(
		"create table foo(id char(8) not null primary key, received int)",
		nil, nil)
	require.NoError(t, err)
	for _, row := range []struct {
		id types.KeyBytes
		ts int64
	}{
		{
			id: types.KeyBytes{0, 0, 0, 1, 0, 0, 0, 0},
			ts: 100,
		},
		{
			id: types.KeyBytes{0, 0, 0, 3, 0, 0, 0, 0},
			ts: 200,
		},
		{
			id: types.KeyBytes{0, 0, 0, 5, 0, 0, 0, 0},
			ts: 300,
		},
		{
			id: types.KeyBytes{0, 0, 0, 7, 0, 0, 0, 0},
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
		seq, err := store.from(ctx, types.KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
		require.NoError(t, err)
		actualIDs, err := types.GetN[types.KeyBytes](seq, 5)
		require.NoError(t, err)
		require.Equal(t, []types.KeyBytes{
			{0, 0, 0, 1, 0, 0, 0, 0},
			{0, 0, 0, 3, 0, 0, 0, 0},
			{0, 0, 0, 5, 0, 0, 0, 0},
			{0, 0, 0, 7, 0, 0, 0, 0},
			{0, 0, 0, 1, 0, 0, 0, 0}, // wrapped around
		}, actualIDs)

		seq, err = store.all(ctx)
		require.NoError(t, err)
		actualIDs1, err := types.GetN[types.KeyBytes](seq, 5)
		require.NoError(t, err)
		require.Equal(t, actualIDs, actualIDs1)

		actualIDs = nil
		seq, count, err := store.since(ctx, types.KeyBytes{0, 0, 0, 0, 0, 0, 0, 0}, 300)
		require.NoError(t, err)
		require.Equal(t, 2, count)
		actualIDs, err = types.GetN[types.KeyBytes](seq, 3)
		require.Equal(t, []types.KeyBytes{
			{0, 0, 0, 5, 0, 0, 0, 0},
			{0, 0, 0, 7, 0, 0, 0, 0},
			{0, 0, 0, 5, 0, 0, 0, 0}, // wrapped around
		}, actualIDs)

		require.NoError(t, store.registerHash(types.KeyBytes{0, 0, 0, 2, 0, 0, 0, 0}))
		require.NoError(t, store.registerHash(types.KeyBytes{0, 0, 0, 9, 0, 0, 0, 0}))
		actualIDs = nil
		seq, err = store.from(ctx, types.KeyBytes{0, 0, 0, 0, 0, 0, 0, 0})
		actualIDs, err = types.GetN[types.KeyBytes](seq, 6)
		require.NoError(t, err)
		require.Equal(t, []types.KeyBytes{
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
