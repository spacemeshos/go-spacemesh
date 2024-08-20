package sql

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		WithNoCheckSchemaDrift(),
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
		WithNoCheckSchemaDrift(),
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
		WithNoCheckSchemaDrift(),
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
		WithNoCheckSchemaDrift(),
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
		WithNoCheckSchemaDrift(),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func execSQL(t *testing.T, db Executor, sql string, col int) (result string) {
	_, err := db.Exec(sql, nil, func(stmt *Statement) bool {
		if col >= 0 {
			result = stmt.ColumnText(col)
		}
		return true
	})
	require.NoError(t, err)
	return result
}

func TestDatabaseVacuumState(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)

	// The first migration is done without vacuuming and thus it is performed
	// in-place.
	migration1 := NewMockMigration(ctrl)
	migration1.EXPECT().Order().Return(1).AnyTimes()
	migration1.EXPECT().Apply(gomock.Any(), gomock.Any()).
		DoAndReturn(func(db Executor, logger *zap.Logger) error {
			require.NotContains(t, execSQL(t, db, "PRAGMA database_list", 2), "_migrate")
			require.Equal(t, "wal", execSQL(t, db, "PRAGMA journal_mode", 0))
			require.Equal(t, "1", execSQL(t, db, "PRAGMA synchronous", 0)) // NORMAL
			execSQL(t, db, "create table foo(x int)", -1)
			return nil
		}).Times(1)

	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any(), gomock.Any()).
		DoAndReturn(func(db Executor, logger *zap.Logger) error {
			// We must be operating on a temp database.
			require.Contains(t, execSQL(t, db, "PRAGMA database_list", 2), "_migrate")
			// Journaling is off for the temp database as it is deleted in case
			// of migration failure.
			require.Equal(t, "off", execSQL(t, db, "PRAGMA journal_mode", 0))
			// Synchronous is off for the temp database as it is deleted in case
			// of migration failure.
			require.Equal(t, "0", execSQL(t, db, "PRAGMA synchronous", 0)) // OFF
			execSQL(t, db, "create table bar(y int)", -1)
			return nil
		}).Times(1)

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Script: "PRAGMA user_version = 1;\n" +
				"CREATE TABLE foo(x int);\n",
			Migrations: MigrationList{migration1},
		}),
		WithForceMigrations(true),
	)
	require.NoError(t, err)
	execSQL(t, db, "select * from foo", -1) // ensure table exists
	require.NoError(t, db.Close())

	db, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Script: "PRAGMA user_version = 2;\n" +
				"CREATE TABLE bar(y int);\n" +
				"CREATE TABLE foo(x int);\n",
			Migrations: MigrationList{migration1, migration2},
		}),
		WithVacuumState(2),
	)
	require.NoError(t, err)
	execSQL(t, db, "select * from foo, bar", -1)
	require.NoError(t, db.Close())

	// The wal file should be absent after the database is re-created
	// with VACUUM INTO
	_, err = os.Open(dbFile + "-wal")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestDatabaseVacuumStateError(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)

	migration1 := &sqlMigration{
		order:   1,
		name:    "0001_initial.sql",
		content: "create table foo(x int)",
	}

	fail := true
	migration2 := NewMockMigration(ctrl)
	migration2.EXPECT().Name().Return("0002_test.sql").AnyTimes()
	migration2.EXPECT().Order().Return(2).AnyTimes()
	migration2.EXPECT().Apply(gomock.Any(), gomock.Any()).
		DoAndReturn(func(db Executor, logger *zap.Logger) error {
			if fail {
				return errors.New("migration failed")
			}
			execSQL(t, db, "create table bar(y int)", -1)
			return nil
		}).Times(2)

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Script: "PRAGMA user_version = 1;\n" +
				"CREATE TABLE foo(x int);\n",
			Migrations: MigrationList{migration1},
		}),
	)
	require.NoError(t, err)
	execSQL(t, db, "select * from foo", -1) // ensure table exists
	require.NoError(t, db.Close())

	schema := &Schema{
		Script: "PRAGMA user_version = 2;\n" +
			"CREATE TABLE bar(y int);\n" +
			"CREATE TABLE foo(x int);\n",
		Migrations: MigrationList{migration1, migration2},
	}
	_, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(schema),
		WithVacuumState(2),
	)
	require.Error(t, err)

	// All temporary files need to be deleted upon migration failure.
	tmpDBFiles, err := filepath.Glob(filepath.Join(dir, "*_migrate*"))
	require.NoError(t, err)
	require.Empty(t, tmpDBFiles)

	// Make sure the initial DB is intact after failed migration,
	// and the 2nd migration is applied on the second attempt.
	fail = false
	db, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(schema),
		WithVacuumState(2),
	)
	require.NoError(t, err)
	execSQL(t, db, "select * from foo, bar", -1)
	require.NoError(t, db.Close())
}

// faultyMigration is a migration that can be configured to panic during Apply.
// We don't use mock for this as it's not entirely clear what happens if a mocked method
// panics.
type faultyMigration struct {
	panic, interceptVacuumInto bool
	*sqlMigration
}

var _ Migration = &faultyMigration{}

func (m *faultyMigration) Apply(db Executor, logger *zap.Logger) error {
	if m.interceptVacuumInto {
		db.(Database).Intercept("crashOnVacuum", func(query string) error {
			if strings.Contains(strings.ToLower(query), "vacuum into") {
				panic("simulated crash")
			}
			return nil
		})
	}
	if m.panic {
		panic("simulated crash")
	}
	return m.sqlMigration.Apply(db, logger)
}

func TestDropIncompleteMigration(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	migration1 := &sqlMigration{
		order:   1,
		name:    "0001_initial.sql",
		content: "create table foo(x int)",
	}
	migration2 := &faultyMigration{
		panic: true,
		sqlMigration: &sqlMigration{
			order:   2,
			name:    "0002_test.sql",
			content: "create table bar(y int)",
		},
	}

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Script: "PRAGMA user_version = 1;\n" +
				"CREATE TABLE foo(x int);\n",
			Migrations: MigrationList{migration1},
		}),
		WithForceMigrations(true),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	schema := &Schema{
		Script: "PRAGMA user_version = 2;\n" +
			"CREATE TABLE bar(y int);\n" +
			"CREATE TABLE foo(x int);\n",
		Migrations: MigrationList{migration1, migration2},
	}

	require.Panics(t, func() {
		Open("file:"+dbFile,
			WithLogger(logger),
			WithDatabaseSchema(schema),
			WithVacuumState(2),
		)
	})

	// Check that temporary database exists after the simulated crash.
	// Note that we're checking "*_migrate" not "*_migrate*" to avoid matching
	// any erroneously created successful migration markers.
	tmpDBFiles, err := filepath.Glob(filepath.Join(dir, "*_migrate"))
	require.NoError(t, err)
	require.NotEmpty(t, tmpDBFiles)

	// Retry migration. The incompletely migrated temporary database should be dropped.
	migration2.panic = false
	db, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(schema),
		WithVacuumState(2),
	)
	require.NoError(t, err)
	execSQL(t, db, "select * from foo, bar", -1)
	require.NoError(t, db.Close())
}

func TestResumeCopyMigration(t *testing.T) {
	dir := t.TempDir()
	logger := zaptest.NewLogger(t)
	migration1 := &sqlMigration{
		order:   1,
		name:    "0001_initial.sql",
		content: "create table foo(x int)",
	}
	// This migration will panic when VACUUM INTO is attempted to copy
	// the migrated database to the source database location.
	migration2 := &faultyMigration{
		interceptVacuumInto: true,
		sqlMigration: &sqlMigration{
			order:   2,
			name:    "0002_test.sql",
			content: "create table bar(y int)",
		},
	}

	dbFile := filepath.Join(dir, "test.sql")
	db, err := Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Script: "PRAGMA user_version = 1;\n" +
				"CREATE TABLE foo(x int);\n",
			Migrations: MigrationList{migration1},
		}),
		WithForceMigrations(true),
	)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	schema := &Schema{
		Script: "PRAGMA user_version = 2;\n" +
			"CREATE TABLE bar(y int);\n" +
			"CREATE TABLE foo(x int);\n",
		Migrations: MigrationList{migration1, migration2},
	}

	require.Panics(t, func() {
		Open("file:"+dbFile,
			WithLogger(logger),
			WithDatabaseSchema(schema),
			WithVacuumState(2),
		)
	})

	// Check that temporary database exists after the simulated crash.
	tmpDBFiles, err := filepath.Glob(filepath.Join(dir, "*"))
	t.Logf("tmpDBFiles: %v", tmpDBFiles)
	require.NoError(t, err)
	require.NotEmpty(t, tmpDBFiles)

	// Retry migration. The migrated database should be copied
	// to the source database location without invoking any further
	// migrations. As the migration with fault injection is not called,
	// the final VACUUM INTO must succeed.
	db, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(schema),
		WithVacuumState(2),
	)
	require.NoError(t, err)
	execSQL(t, db, "select * from foo, bar", -1)
	require.NoError(t, db.Close())
}

func TestDBClosed(t *testing.T) {
	db := InMemory(WithLogger(zaptest.NewLogger(t)), WithNoCheckSchemaDrift())
	require.NoError(t, db.Close())
	_, err := db.Exec("select 1", nil, nil)
	require.ErrorIs(t, err, ErrClosed)
	err = db.WithTx(context.Background(), func(tx Transaction) error { return nil })
	require.ErrorIs(t, err, ErrClosed)
}

func TestQueryCount(t *testing.T) {
	db := InMemory(WithLogger(zaptest.NewLogger(t)), WithNoCheckSchemaDrift())
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
		WithNoCheckSchemaDrift())
	require.NoError(t, err)
	_, err = db.Exec("PRAGMA user_version = 3", nil, nil)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = Open("file:"+dbFile,
		WithLogger(logger),
		WithDatabaseSchema(&Schema{
			Migrations: MigrationList{migration1, migration2},
		}),
		WithNoCheckSchemaDrift(),
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

// TBD: test WAL modes for temp DB
// TBD: remove SQLITE_OPEN_WAL from open flags and check journal mode
