package statesql

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestDatabase_MigrateTwice_NoOp(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.db")
	db, err := Open("file:"+file, sql.WithForceMigrations(true))
	require.NoError(t, err)

	sql1, err := sql.LoadDBSchemaScript(db)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db, err = Open("file:" + file)
	require.NoError(t, err)
	sql2, err := sql.LoadDBSchemaScript(db)
	require.NoError(t, err)

	require.Equal(t, sql1, sql2)

	var version int
	_, err = db.Exec("PRAGMA user_version;", nil, func(stmt *sql.Statement) bool {
		version = stmt.ColumnInt(0)
		return true
	})
	require.NoError(t, err)

	require.NoError(t, err)
	schema, err := Schema()
	require.NoError(t, err)
	expectedVersion := slices.MaxFunc(
		[]sql.Migration(schema.Migrations),
		func(a, b sql.Migration) int {
			return a.Order() - b.Order()
		})
	require.Equal(t, expectedVersion.Order(), version)

	require.NoError(t, db.Close())
}

func TestSchema(t *testing.T) {
	for _, tc := range []struct {
		name            string
		forceMigrations bool
	}{
		{name: "no migrations", forceMigrations: false},
		{name: "force migrations", forceMigrations: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := InMemory(sql.WithForceMigrations(tc.forceMigrations))
			loadedScript, err := sql.LoadDBSchemaScript(db)
			require.NoError(t, err)
			expSchema, err := Schema()
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
