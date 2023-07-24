package sql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigrationsAppliedOnce(t *testing.T) {
	db := InMemory()

	var version int
	_, err := db.Exec("PRAGMA user_version;", nil, func(stmt *Statement) bool {
		version = stmt.ColumnInt(0)
		return true
	})
	require.NoError(t, err)
	require.Equal(t, version, 2)
}
