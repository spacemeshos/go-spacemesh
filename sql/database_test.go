package sql

import (
	"context"
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

func TestTransactionIsolation(t *testing.T) {
	db := InMemory(
		WithMigrations(testTables),
		WithConnections(10),
		WithLatencyMetering(true),
	)

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
