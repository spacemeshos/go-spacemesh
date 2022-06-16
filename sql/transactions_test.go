package sql

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/spacemeshos/sqlite"
	"github.com/stretchr/testify/require"
)

func testTable1(db Executor) error {
	if _, err := db.Exec(`create table testing1 (
		id varchar primary key,
		field int
	)`, nil, nil); err != nil {
		return err
	}
	return nil
}

func testTable2(db Executor) error {
	if _, err := db.Exec(`create table testing2 (
		id varchar primary key,
		field int
	)`, nil, nil); err != nil {
		return err
	}
	return nil
}

func persistentDB(tb testing.TB) *Database {
	tb.Helper()
	db, err := Open(testURI(tb), WithMigrations(testTable1))
	require.NoError(tb, err)
	return db
}

func testURI(tb testing.TB) string {
	tb.Helper()
	return "file:" + filepath.Join(tb.TempDir(), "state.sql")
}

func TestTransactionIsolation(t *testing.T) {
	t.Parallel()
	db := InMemory(WithMigrations(testTable1))

	tx, err := db.Tx(context.TODO())
	require.NoError(t, err)

	key := "dsada"
	val := int64(20)
	_, err = tx.Exec("insert into testing1(id, field) values (?1, ?2)", func(stmt *Statement) {
		stmt.BindText(1, key)
		stmt.BindInt64(2, val)
	}, nil)
	require.NoError(t, err)

	require.True(t, checkValueInTable(t, tx, "testing1", key, val))
	require.NoError(t, tx.Release())
	require.False(t, checkValueInTable(t, db, "testing1", key, val))
}

func TestConcurrentTransactions(t *testing.T) {
	t.Parallel()
	// create 2 DIFFERENT tables.
	// If we do operate on the same table, after commit of first transaction will be ok.
	// But when we will commit 2nd transaction, it will fail with error `SQLITE_BUSY_SNAPSHOT`.
	// It causes because lock is deferred to COMMIT,
	// so before commit it asks if the transaction being committed operates on a different set of data
	// than all other concurrently executing transactions
	db, err := Open(testURI(t), WithMigrations(func(executor Executor) error {
		require.NoError(t, testTable1(executor))
		require.NoError(t, testTable2(executor))
		return nil
	}))
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
	require.True(t, checkValueInTable(t, db, "testing1", "space", 1))
	require.True(t, checkValueInTable(t, db, "testing2", "mesh", 2))
}

func TestDatabase_WithTx(t *testing.T) {
	t.Parallel()
	db, err := Open(testURI(t), WithMigrations(testTable1))
	require.NoError(t, err)
	require.NoError(t, insertInTX(db, "space", 1, []TxRetryOpt{}))
	require.True(t, checkValueInTable(t, db, "testing1", "space", 1))
}

func TestDatabase_TxConcurrent(t *testing.T) {
	t.Parallel()
	db, err := Open(testURI(t), WithMigrations(testTable1))
	require.NoError(t, err)

	require.NoError(t, insertInTXConcurrent(db, "space", 1, []TxRetryOpt{}))
	require.True(t, checkValueInTable(t, db, "testing1", "space", 1))
}

func TestDatabase_RetryTx(t *testing.T) {
	t.Parallel()
	t.Run("success transactions in parallel", func(t *testing.T) {
		t.Parallel()
		db, err := Open(testURI(t), WithMigrations(testTable1))
		require.NoError(t, err)
		opts := []TxRetryOpt{WithRetryBusy(3), WithRetryTimeout(100)}
		// run in parallel 2 tx
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			require.NoError(t, insertInTX(db, "space", 1, opts))
		}()

		go func() {
			defer wg.Done()
			<-start
			require.NoError(t, insertInTX(db, "mesh", 2, opts))
		}()
		close(start)
		wg.Wait()
		require.True(t, checkValueInTable(t, db, "testing1", "space", 1))
		require.True(t, checkValueInTable(t, db, "testing1", "mesh", 2))
	})

	t.Run("fail transactions in parallel", func(t *testing.T) {
		t.Parallel()
		db, err := Open(testURI(t), WithMigrations(testTable1))
		require.NoError(t, err)

		// run in parallel 2 tx
		start, txOne, txTwo := make(chan struct{}), make(chan struct{}), make(chan struct{})

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			err = db.WithTx(context.TODO(), func(tx Executor) error {
				close(txOne)
				_, err = tx.Exec("insert into testing1(id, field) values (?1, ?2)", func(stmt *Statement) {
					stmt.BindText(1, "space")
					stmt.BindInt64(2, 1)
				}, nil)
				<-txTwo
				require.NoError(t, err)
				return nil
			}, []TxRetryOpt{WithRetryBusy(1), WithRetryTimeout(0)}...)
			require.NoError(t, err)
		}()

		go func() {
			defer wg.Done()
			<-start
			<-txOne
			errI := insertInTX(db, "mesh", 2, []TxRetryOpt{WithRetryBusy(1), WithRetryTimeout(0)})
			require.NotNil(t, errI)
			require.Equal(t, sqlite.SQLITE_BUSY, db.GetSQLiteError(errI).Code)
			close(txTwo)
		}()

		close(start)
		wg.Wait()
		firstTx := checkValueInTable(t, db, "testing1", "space", 1)
		secondTx := checkValueInTable(t, db, "testing1", "mesh", 2)
		require.True(t, firstTx != secondTx, "there can be only one!")
	})
}

func TestDatabase_RetryTxConcurrent(t *testing.T) {
	t.Parallel()
	t.Run("success concurrent transactions in parallel", func(t *testing.T) {
		t.Parallel()
		db, err := Open(testURI(t), WithMigrations(testTable1))
		require.NoError(t, err)
		opts := []TxRetryOpt{WithRetryBusy(10), WithRetryTimeout(100)}
		// run in parallel 2 tx
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			require.NoError(t, insertInTXConcurrent(db, "space", 1, opts))
		}()

		go func() {
			defer wg.Done()
			<-start
			require.NoError(t, insertInTXConcurrent(db, "mesh", 2, opts))
		}()
		close(start)
		wg.Wait()
		require.True(t, checkValueInTable(t, db, "testing1", "space", 1))
		require.True(t, checkValueInTable(t, db, "testing1", "mesh", 2))
	})
	t.Run("fail concurrent transactions in parallel", func(t *testing.T) {
		t.Parallel()
		db, err := Open(testURI(t), WithMigrations(func(executor Executor) error {
			require.NoError(t, testTable1(executor))
			require.NoError(t, testTable2(executor))
			return nil
		}))
		opts := []TxRetryOpt{WithRetryBusy(10), WithRetryTimeout(100)}
		require.NoError(t, err)

		// run in parallel 2 tx
		start, txOne, txTwo := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			<-start
			err = db.WithTxConcurrent(context.TODO(), func(tx Executor) error {
				close(txOne)
				_, err = tx.Exec("insert into testing2(id, field) values (?1, ?2)", func(stmt *Statement) {
					stmt.BindText(1, "space")
					stmt.BindInt64(2, 1)
				}, nil)
				<-txTwo
				require.NoError(t, err)
				return nil
			}, []TxRetryOpt{WithRetryBusy(1), WithRetryTimeout(0)}...)
			require.NoError(t, err)
		}()

		go func() {
			defer wg.Done()
			<-start
			<-txOne
			require.NoError(t, insertInTXConcurrent(db, "space", 1, opts))
			close(txTwo)
		}()
		close(start)
		wg.Wait()
		require.True(t, checkValueInTable(t, db, "testing1", "space", 1))
		require.True(t, checkValueInTable(t, db, "testing2", "space", 1))
	})
}

func insertInTXConcurrent(db *Database, id string, field int64, opts []TxRetryOpt) error {
	return db.WithTx(context.TODO(), func(tx Executor) error {
		_, err := tx.Exec("insert into testing1(id, field) values (?1, ?2)", func(stmt *Statement) {
			stmt.BindText(1, id)
			stmt.BindInt64(2, field)
		}, nil)
		return err
	}, opts...)
}

func insertInTX(db *Database, id string, field int64, opts []TxRetryOpt) error {
	return db.WithTx(context.TODO(), func(tx Executor) error {
		_, err := tx.Exec("insert into testing1(id, field) values (?1, ?2)", func(stmt *Statement) {
			stmt.BindText(1, id)
			stmt.BindInt64(2, field)
		}, nil)
		return err
	}, opts...)
}

func checkValueInTable(t *testing.T, db Executor, tableName, id string, field int64) bool {
	rows, err := db.Exec(fmt.Sprintf("select 1 from %s where id = ?1 AND field = ?2", tableName), func(stmt *Statement) {
		stmt.BindText(1, id)
		stmt.BindInt64(2, field)
	}, nil)
	require.NoError(t, err)
	return rows > 0
}
