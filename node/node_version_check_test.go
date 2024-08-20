package node

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestUpgradeToV15(t *testing.T) {
	t.Run("fresh installation - no local DB file", func(t *testing.T) {
		cfg := config.DefaultTestConfig()
		cfg.DataDirParent = t.TempDir()

		require.NoError(t, verifyLocalDbMigrations(&cfg))
	})

	t.Run("migrated DB passes", func(t *testing.T) {
		cfg := config.DefaultTestConfig()
		cfg.DataDirParent = t.TempDir()

		uri := path.Join(cfg.DataDir(), localDbFile)
		localDb, err := localsql.Open(uri)
		require.NoError(t, err)
		localDb.Close()

		require.NoError(t, verifyLocalDbMigrations(&cfg))
	})

	t.Run("not fully migrated DB fails", func(t *testing.T) {
		cfg := config.DefaultTestConfig()
		cfg.DataDirParent = t.TempDir()

		uri := path.Join(cfg.DataDir(), localDbFile)

		schema, err := statesql.Schema()
		require.NoError(t, err)

		schema.Migrations = schema.Migrations[:2]

		db, err := statesql.Open(uri,
			sql.WithDatabaseSchema(schema),
			sql.WithForceMigrations(true),
			sql.WithNoCheckSchemaDrift())
		require.NoError(t, err)
		require.NoError(t, db.Close())

		require.ErrorContains(t, verifyLocalDbMigrations(&cfg), "please upgrade")
	})
}
