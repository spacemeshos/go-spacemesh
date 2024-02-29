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
	require.Equal(t, rows, 1)

	require.NoError(t, tx.Release())

	rows, err = db.Exec("select 1 from testing1 where id = ?1", func(stmt *Statement) {
		stmt.BindText(1, key)
	}, nil)
	require.NoError(t, err)
	require.Equal(t, rows, 0)
}

func Test_Migration_Rollback(t *testing.T) {
	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Name().Return("test").AnyTimes()
	migration1.EXPECT().Order().Return(1).AnyTimes()

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Name().Return("test").AnyTimes()
	migration2.EXPECT().Order().Return(2).AnyTimes()

	// migration1 should be rolled back when migration2 fails if both are applied in the same transaction
	migration1.EXPECT().Apply(gomock.Any()).Return(nil)
	migration2.EXPECT().Apply(gomock.Any()).Return(errors.New("migration 2 failed"))

	migration1.EXPECT().Rollback().Return(nil)
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
	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:" + dbFile)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db, err = Open("file:"+dbFile,
		WithMigrations([]Migration{}),
		WithVacuumState(11),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// we run pragma wal_checkpoint(TRUNCATE) after vacuum, which drops the wal file
	_, err = os.Open(dbFile + "-wal")
	require.ErrorIs(t, err, os.ErrNotExist)
}
