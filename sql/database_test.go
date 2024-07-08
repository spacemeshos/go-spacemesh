package sql

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

func Test_Transaction_Isolation(t *testing.T) {
	db := InMemory(
		WithLogger(zaptest.NewLogger(t)),
		WithConnections(10),
		WithLatencyMetering(true),
		WithDatabaseSchema(&Schema{
			Script: `create table testing1 (
				id varchar primary key,
				field int
			);`,
		}),
		WithIgnoreSchemaDrift(),
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

	migration1.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil)
	migration2.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(errors.New("migration 2 failed"))

	migration2.EXPECT().Rollback().Return(nil)

	dbFile := filepath.Join(t.TempDir(), "test.sql")
	_, err := Open("file:"+dbFile,
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1, migration2},
		}),
		WithForceMigrations(true),
	)
	require.ErrorContains(t, err, "migration 2 failed")
}

func Test_Migration_Rollback_Only_NewMigrations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Name().Return("test").AnyTimes()
	migration1.EXPECT().Order().Return(1).AnyTimes()
	migration1.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil)

	dbFile := filepath.Join(t.TempDir(), "test.sql")
	db, err := Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1},
		}),
		WithForceMigrations(true),
		WithIgnoreSchemaDrift(),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Name().Return("test").AnyTimes()
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(errors.New("migration 2 failed"))
	migration2.EXPECT().Rollback().Return(nil)

	_, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1, migration2},
		}),
	)
	require.ErrorContains(t, err, "migration 2 failed")
}

func Test_Migration_Disabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Name().Return("test").AnyTimes()
	migration1.EXPECT().Order().Return(1).AnyTimes()
	migration1.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil)

	dbFile := filepath.Join(t.TempDir(), "test.sql")
	db, err := Open("file:"+dbFile,
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1},
		}),
		WithForceMigrations(true),
		WithIgnoreSchemaDrift(),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Order().Return(2).AnyTimes()

	_, err = Open("file:"+dbFile,
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1, migration2},
		}),
		WithMigrationsDisabled(),
	)
	require.ErrorIs(t, err, ErrOldSchema)
}

func TestDatabaseSkipMigrations(t *testing.T) {
	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Order().Return(1).AnyTimes()

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Name().Return("test").AnyTimes()
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil)

	schema := &Schema{
		Migrations: MigrationList{migration1, migration2},
	}
	schema.SkipMigrations(1)
	dbFile := filepath.Join(t.TempDir(), "test.sql")
	db, err := Open("file:"+dbFile,
		WithDatabaseSchema(schema),
		WithForceMigrations(true),
		WithIgnoreSchemaDrift(),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func TestDatabaseVacuumState(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Order().Return(1).AnyTimes()
	migration1.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(nil).Times(1)

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1},
		}),
		WithForceMigrations(true),
		WithIgnoreSchemaDrift(),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1, migration2},
		}),
		WithVacuumState(2),
		WithIgnoreSchemaDrift(),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// we run pragma wal_checkpoint(TRUNCATE) after vacuum, which drops the wal file
	_, err = os.Open(dbFile + "-wal")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestQueryCount(t *testing.T) {
	db := InMemory(WithLogger(zaptest.NewLogger(t)), WithIgnoreSchemaDrift())
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
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Order().Return(1).AnyTimes()

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Order().Return(2).AnyTimes()

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:"+dbFile,
		WithLogger(logger),
		WithForceMigrations(true),
		WithIgnoreSchemaDrift())
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 3", nil, nil)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1, migration2},
		}),
		WithIgnoreSchemaDrift(),
	)
	require.ErrorIs(t, err, ErrTooNew)
}

func TestSchemaDrift(t *testing.T) {
	observer, observedLogs := observer.New(zapcore.WarnLevel)
	logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))
	dbFile := filepath.Join(t.TempDir(), "test.sql")
	schema := &Schema{
		// Not using ` here to avoid schema drift warnings due to whitespace
		// TODO: ignore whitespace and comments during schema comparison
		Script: "PRAGMA user_version = 0;\n" +
			"CREATE TABLE testing1 (\n" +
			" id varchar primary key,\n" +
			" field int\n" +
			");\n",
	}
	db, err := Open("file:"+dbFile,
		WithDatabaseSchema(schema),
		WithLogger(logger),
	)
	require.NoError(t, err)

	_, err = db.Exec("create table newtbl (id int)", nil, nil)
	require.NoError(t, err)

	require.NoError(t, db.Close())
	require.Equal(t, 0, observedLogs.Len(), "expected 0 log messages")

	_, err = Open("file:"+dbFile,
		WithDatabaseSchema(schema),
		WithLogger(logger),
	)
	require.Error(t, err)
	require.Regexp(t, `.*\n.*\+.*CREATE TABLE newtbl \(id int\);`, err.Error())
	require.Equal(t, 0, observedLogs.Len(), "expected 0 log messages")

	db, err = Open("file:"+dbFile,
		WithDatabaseSchema(schema),
		WithLogger(logger),
		WithAllowSchemaDrift(true),
	)
	require.NoError(t, db.Close())
	require.NoError(t, err)
	require.Equal(t, 1, observedLogs.Len(), "expected 1 log messages")
	require.Equal(t, "database schema drift detected", observedLogs.All()[0].Message)
	require.Regexp(t, `.*\n.*\+.*CREATE TABLE newtbl \(id int\);`,
		observedLogs.All()[0].ContextMap()["diff"])
}
