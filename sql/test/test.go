package test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"

	"github.com/spacemeshos/go-spacemesh/sql"
)

type Database interface {
	sql.Executor
	Close() error
}

type DBFuncs[DB Database] interface {
	Schema() (*sql.Schema, error)
	Open(uri string, opts ...sql.Opt) (DB, error)
	InMemory(opts ...sql.Opt) DB
}

func RunSchemaTests[DB Database](t *testing.T, funcs DBFuncs[DB]) {
	t.Run("idempotent migration", func(t *testing.T) {
		observer, observedLogs := observer.New(zapcore.InfoLevel)
		logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
			func(core zapcore.Core) zapcore.Core {
				return zapcore.NewTee(core, observer)
			},
		)))

		file := filepath.Join(t.TempDir(), "test.db")
		db, err := funcs.Open("file:"+file, sql.WithForceMigrations(true), sql.WithLogger(logger))
		require.NoError(t, err)

		var versionA int
		_, err = db.Exec("PRAGMA user_version", nil, func(stmt *sql.Statement) bool {
			versionA = stmt.ColumnInt(0)
			return true
		})
		require.NoError(t, err)

		sql1, err := sql.LoadDBSchemaScript(db, "")
		require.NoError(t, err)
		require.NoError(t, db.Close())

		require.Equal(t, 1, observedLogs.Len(), "expected 1 log messages")
		l := observedLogs.All()[0]
		require.Equal(t, "running migrations", l.Message)
		require.Equal(t, int64(0), l.ContextMap()["current version"])
		require.Equal(t, int64(versionA), l.ContextMap()["target version"])

		db, err = funcs.Open("file:"+file, sql.WithLogger(logger))
		require.NoError(t, err)
		sql2, err := sql.LoadDBSchemaScript(db, "")
		require.NoError(t, err)

		require.Equal(t, sql1, sql2)

		var versionB int
		_, err = db.Exec("PRAGMA user_version", nil, func(stmt *sql.Statement) bool {
			versionB = stmt.ColumnInt(0)
			return true
		})
		require.NoError(t, err)

		require.NoError(t, err)
		schema, err := funcs.Schema()
		require.NoError(t, err)
		expectedVersion := slices.MaxFunc(
			[]sql.Migration(schema.Migrations),
			func(a, b sql.Migration) int {
				return a.Order() - b.Order()
			})
		require.Equal(t, expectedVersion.Order(), versionA)
		require.Equal(t, expectedVersion.Order(), versionB)

		require.NoError(t, db.Close())
		// make sure there's no schema drift warnings in the logs
		require.Equal(t, 1, observedLogs.Len(), "expected 1 log message")
	})

	t.Run("schema", func(t *testing.T) {
		for _, tc := range []struct {
			name            string
			forceMigrations bool
		}{
			{name: "no migrations", forceMigrations: false},
			{name: "force migrations", forceMigrations: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				db := funcs.InMemory(sql.WithForceMigrations(tc.forceMigrations))
				loadedScript, err := sql.LoadDBSchemaScript(db, "")
				require.NoError(t, err)
				expSchema, err := funcs.Schema()
				require.NoError(t, err)
				diff := expSchema.Diff(loadedScript)
				if diff != "" {
					s := &sql.Schema{Script: loadedScript}
					require.NoError(t, s.WriteToFile("."))
					t.Logf("updated schema written to %s", sql.UpdatedSchemaPath)
				}
				require.Empty(t, diff, "schema diff")
			})
		}
	})
}
