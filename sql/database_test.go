package sql

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func testTables(db Executor) error {
	if _, err := db.Exec(`create table testing1 (
		id varchar primary key,
		field int
	)`, nil, nil); err != nil {
		return err
	}
	return nil
}

func persistentDB(tb testing.TB) *Database {
	tb.Helper()
	db, err := Open(testURI(tb), WithMigrations(testTables))
	require.NoError(tb, err)
	return db
}

func testURI(tb testing.TB) string {
	tb.Helper()
	return "file:" + filepath.Join(tb.TempDir(), "state.sql")
}

func TestTransactionIsolation(t *testing.T) {
	db := InMemory(WithMigrations(testTables))

	tx, err := db.Tx(context.TODO())
	require.NoError(t, err)

	key := "dsada"
	_, err = tx.Exec("insert into testing1(id, field) values (?1, ?2)", func(stmt *Statement) {
		stmt.BindText(1, key)
		stmt.BindInt64(2, 20)
	}, nil)
	require.NoError(t, err)

	rows, err := tx.Exec("select 1 from testing1 where id = ?1", func(stmt *Statement) {
		stmt.BindText(1, key)
	}, nil)
	require.NoError(t, err)
	require.Equal(t, rows, 1)

	require.NoError(t, tx.Release())

	rows, err = db.Exec("select 1 from testing1 where id = ?1", func(stmt *Statement) {
		stmt.BindText(1, key)
	}, nil)
	require.NoError(t, err)
	require.Equal(t, rows, 0)
}

func TestConcurrentTransactions(t *testing.T) {
	db, err := Open(testURI(t))
	require.NoError(t, err)

	// create 2 DIFFERENT tables.
	// If we do operate on the same table, after commit of first transaction will be ok.
	// But when we will commit 2nd transaction, it will fail with error `SQLITE_BUSY_SNAPSHOT`.
	// It causes because lock is deferred to COMMIT,
	// so before commit it asks if the transaction being committed operates on a different set of data
	// than all other concurrently executing transactions
	_, err = db.Exec("create table testing1 (id varchar primary key, field int)", nil, nil)
	require.NoError(t, err)

	_, err = db.Exec("create table testing2 (id varchar primary key, field int)", nil, nil)
	require.NoError(t, err)

	dbTx1, err := db.TxConcurrent(context.TODO())
	require.NoError(t, err)
	dbTx2, err := db.TxConcurrent(context.TODO())
	require.NoError(t, err)

	_, err = dbTx1.Exec("insert into testing1(id, field) values (?1, ?2)", func(stmt *Statement) {
		stmt.BindText(1, "space")
		stmt.BindInt64(2, 1)
	}, nil)
	require.NoError(t, err)

	_, err = dbTx2.Exec("insert into testing2(id, field) values (?1, ?2)", func(stmt *Statement) {
		stmt.BindText(1, "mesh")
		stmt.BindInt64(2, 2)
	}, nil)
	require.NoError(t, err)

	require.NoError(t, dbTx1.Commit())
	require.NoError(t, dbTx2.Commit())

	// check that data in db
	_, err = db.Exec("select id, field from testing1", nil, func(stmt *Statement) bool {
		require.Equal(t, "space", stmt.ColumnText(0))
		require.Equal(t, int64(1), stmt.ColumnInt64(1))
		return true
	})
	require.NoError(t, err)

	_, err = db.Exec("select id, field from testing2", nil, func(stmt *Statement) bool {
		require.Equal(t, "mesh", stmt.ColumnText(0))
		require.Equal(t, int64(2), stmt.ColumnInt64(1))
		return true
	})
	require.NoError(t, err)
}
