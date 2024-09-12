package sql

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func Test_Transaction_Isolation(t *testing.T) {
	ctrl := gomock.NewController(t)
	testMigration := NewMockMigration(ctrl)
	testMigration.EXPECT().Name().Return("test").AnyTimes()
	testMigration.EXPECT().Order().Return(1).AnyTimes()
	testMigration.EXPECT().Apply(gomock.Any()).DoAndReturn(func(e Executor) error {
		if _, err := e.Exec(`create table testing1 (
			id varchar primary key,
			field int
		)`, nil, nil); err != nil {
			return err
		}
		return nil
	})

	db := InMemory(
		WithMigrations([]Migration{testMigration}),
		WithConnections(10),
		WithLatencyMetering(true),
	)

	tx, err := db.Tx(context.Background())
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
	require.Equal(t, 1, rows)

	require.NoError(t, tx.Release())

	rows, err = db.Exec("select 1 from testing1 where id = ?1", func(stmt *Statement) {
		stmt.BindText(1, key)
	}, nil)
	require.NoError(t, err)
	require.Equal(t, 0, rows)
}

func Test_Migration_Rollback(t *testing.T) {
	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Name().Return("test").AnyTimes()
	migration1.EXPECT().Order().Return(1).AnyTimes()

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Name().Return("test").AnyTimes()
	migration2.EXPECT().Order().Return(2).AnyTimes()

	migration1.EXPECT().Apply(gomock.Any()).Return(nil)
	migration2.EXPECT().Apply(gomock.Any()).Return(errors.New("migration 2 failed"))

	migration2.EXPECT().Rollback().Return(nil)

	dbFile := filepath.Join(t.TempDir(), "test.sql")
	_, err := Open("file:"+dbFile,
		WithMigrations([]Migration{migration1, migration2}),
	)
	require.ErrorContains(t, err, "migration 2 failed")
}

func Test_Migration_Rollback_Only_NewMigrations(t *testing.T) {
	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Name().Return("test").AnyTimes()
	migration1.EXPECT().Order().Return(1).AnyTimes()
	migration1.EXPECT().Apply(gomock.Any()).Return(nil)

	dbFile := filepath.Join(t.TempDir(), "test.sql")
	db, err := Open("file:"+dbFile,
		WithMigrations([]Migration{migration1}),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Name().Return("test").AnyTimes()
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any()).Return(errors.New("migration 2 failed"))
	migration2.EXPECT().Rollback().Return(nil)

	_, err = Open("file:"+dbFile,
		WithMigrations([]Migration{migration1, migration2}),
	)
	require.ErrorContains(t, err, "migration 2 failed")
}

func TestDatabaseSkipMigrations(t *testing.T) {
	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Order().Return(1).AnyTimes()

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Name().Return("test").AnyTimes()
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any()).Return(nil)

	dbFile := filepath.Join(t.TempDir(), "test.sql")
	db, err := Open("file:"+dbFile,
		WithMigrations([]Migration{migration1, migration2}),
		WithSkipMigrations(1),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func TestDatabaseVacuumState(t *testing.T) {
	dir := t.TempDir()

	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Order().Return(1).AnyTimes()
	migration1.EXPECT().Apply(gomock.Any()).Return(nil).Times(1)

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any()).Return(nil).Times(1)

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:"+dbFile,
		WithMigrations([]Migration{migration1}),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db, err = Open("file:"+dbFile,
		WithMigrations([]Migration{migration1, migration2}),
		WithVacuumState(2),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// we run pragma wal_checkpoint(TRUNCATE) after vacuum, which drops the wal file
	_, err = os.Open(dbFile + "-wal")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestQueryCount(t *testing.T) {
	db := InMemory()
	require.Equal(t, 0, db.QueryCount())

	n, err := db.Exec("select 1", nil, nil)
	require.Equal(t, 1, n)
	require.NoError(t, err)
	require.Equal(t, 1, db.QueryCount())

	_, err = db.Exec("select invalid", nil, nil)
	require.Error(t, err)
	require.Equal(t, 2, db.QueryCount())
}

func Test_Migration_FailsIfDatabaseTooNew(t *testing.T) {
	dir := t.TempDir()

	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Order().Return(1).AnyTimes()

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Order().Return(2).AnyTimes()

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:" + dbFile)
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 3", nil, nil)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Open("file:"+dbFile,
		WithMigrations([]Migration{migration1, migration2}),
	)
	require.ErrorIs(t, err, ErrTooNew)
}

func TestDatabaseCleanup(t *testing.T) {
	db, err := Open("file:/home/dd/spacemesh/state.sql")
	require.NoError(t, err)
	exec(db.getConn(context.Background()), "delete from ballots where layer < 100000", nil, nil)
}
