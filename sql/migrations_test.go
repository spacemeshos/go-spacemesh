package sql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationsAppliedOnce(t *testing.T) {
	db, err := Open(testURI(t))
	require.NoError(t, err)
	defer db.Close()

	var version int
	_, err = db.Exec("PRAGMA user_version;", nil, func(stmt *Statement) bool {
		version = stmt.ColumnInt(0)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, version, 1)

	require.NoError(t, db.Close())

	db, err = Open(testURI(t))
	require.NoError(t, err)
	defer db.Close()
	require.NotNil(t, db)
}
