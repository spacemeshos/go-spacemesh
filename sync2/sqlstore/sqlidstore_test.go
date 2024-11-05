package sqlstore_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

func TestSQLIdStore(t *testing.T) {
	const keyLen = 12
	db := sql.InMemoryTest(t)
	_, err := db.Exec(
		fmt.Sprintf("create table foo(id char(%d) not null primary key, received int)", keyLen),
		nil, nil)
	require.NoError(t, err)
	for _, row := range []struct {
		id rangesync.KeyBytes
		ts int64
	}{
		{
			id: rangesync.KeyBytes{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
			ts: 100,
		},
		{
			id: rangesync.KeyBytes{0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0},
			ts: 200,
		},
		{
			id: rangesync.KeyBytes{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0},
			ts: 300,
		},
		{
			id: rangesync.KeyBytes{0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0},
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
	st := sqlstore.SyncedTable{
		TableName:       "foo",
		IDColumn:        "id",
		TimestampColumn: "received",
	}
	sts, err := st.Snapshot(db)
	require.NoError(t, err)

	store := sqlstore.NewSQLIDStore(db, sts, keyLen)
	actualIDs, err := store.From(rangesync.KeyBytes{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 5).FirstN(5)
	require.NoError(t, err)
	require.Equal(t, []rangesync.KeyBytes{
		{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}, // wrapped around
	}, actualIDs)

	actualIDs1, err := store.All().FirstN(5)
	require.NoError(t, err)
	require.Equal(t, actualIDs, actualIDs1)

	sr, count := store.Since(rangesync.KeyBytes{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 300)
	require.Equal(t, 2, count)
	actualIDs, err = sr.FirstN(3)
	require.NoError(t, err)
	require.Equal(t, []rangesync.KeyBytes{
		{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0}, // wrapped around
	}, actualIDs)
}
