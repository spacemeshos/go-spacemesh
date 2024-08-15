package statesql

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

func TestIdempotentMigration(t *testing.T) {
	observer, observedLogs := observer.New(zapcore.InfoLevel)
	logger := zaptest.NewLogger(t, zaptest.WrapOptions(zap.WrapCore(
		func(core zapcore.Core) zapcore.Core {
			return zapcore.NewTee(core, observer)
		},
	)))

	file := filepath.Join(t.TempDir(), "test.db")
	db, err := Open("file:"+file, sql.WithForceMigrations(true), sql.WithLogger(logger))
	require.NoError(t, err)

	var versionA int
	_, err = db.Exec("PRAGMA user_version", nil, func(stmt *sql.Statement) bool {
		versionA = stmt.ColumnInt(0)
		return true
	})
	require.NoError(t, err)

	sql1, err := sql.LoadDBSchemaScript(db)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// "running migrations"
	require.Equal(t, 1, observedLogs.Len(), "expected count of log messages")
	l := observedLogs.All()[0]
	require.Equal(t, "running migrations in-place", l.Message)
	require.Equal(t, int64(0), l.ContextMap()["current version"])
	require.Equal(t, int64(versionA), l.ContextMap()["target version"])

	db, err = Open("file:"+file, sql.WithLogger(logger))
	require.NoError(t, err)
	sql2, err := sql.LoadDBSchemaScript(db)
	require.NoError(t, err)

	require.Equal(t, sql1, sql2)

	var versionB int
	_, err = db.Exec("PRAGMA user_version", nil, func(stmt *sql.Statement) bool {
		versionB = stmt.ColumnInt(0)
		return true
	})
	require.NoError(t, err)

	schema, err := Schema()
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
}
