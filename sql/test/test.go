package test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
)

type Database interface {
	sql.Executor
	Close() error
}

type DBFuncs[DB Database] struct {
	Schema   func() (*sql.Schema, error)
	Open     func(uri string, opts ...sql.Opt) (DB, error)
	InMemory func(opts ...sql.Opt) DB
}

func VerifyMigrateTwiceNoOp[DB Database](t *testing.T, funcs DBFuncs[DB]) {
	file := filepath.Join(t.TempDir(), "test.db")
	db, err := funcs.Open("file:"+file, sql.WithForceMigrations(true))
	require.NoError(t, err)

	sql1, err := sql.LoadDBSchemaScript(db, "")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db, err = funcs.Open("file:" + file)
	require.NoError(t, err)
	sql2, err := sql.LoadDBSchemaScript(db, "")
	require.NoError(t, err)

	require.Equal(t, sql1, sql2)

	var version int
	_, err = db.Exec("PRAGMA user_version;", nil, func(stmt *sql.Statement) bool {
		version = stmt.ColumnInt(0)
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
	require.Equal(t, expectedVersion.Order(), version)

	require.NoError(t, db.Close())
}

func VerifySchema[DB Database](t *testing.T, funcs DBFuncs[DB]) {
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
}
