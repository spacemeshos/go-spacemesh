package sql

import (
	"slices"
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

	migrations, err := StateMigrations()
	require.NoError(t, err)
	lastMigration := slices.MaxFunc(migrations, func(a, b Migration) int {
		return a.Order() - b.Order()
	})
	require.Equal(t, lastMigration.Order(), version)
}
