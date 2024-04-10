package node

import (
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
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

		migrations, err := sql.LocalMigrations()
		require.NoError(t, err)

		db, err := sql.Open(uri, sql.WithMigrations(migrations[:2]))
		require.NoError(t, err)
		require.NoError(t, db.Close())

		require.ErrorContains(t, verifyLocalDbMigrations(&cfg), "please upgrade")
	})
}
