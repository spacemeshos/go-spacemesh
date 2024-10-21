package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func TestCodedMigrations(t *testing.T) {
	schema, err := SchemaWithInCodeMigrations(config.DefaultConfig())
	require.NoError(t, err)

	db := sql.InMemory(
		sql.WithDatabaseSchema(schema),
		sql.WithLogger(zaptest.NewLogger(t)),
		sql.WithForceMigrations(true),
	)
	require.NotNil(t, db)
}
