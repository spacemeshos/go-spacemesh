package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestCodedMigrations(t *testing.T) {
	schema, err := localsql.Schema(
	// add coded migrations here
	)
	require.NoError(t, err)

	db := sql.InMemory(
		sql.WithDatabaseSchema(schema),
		sql.WithLogger(zaptest.NewLogger(t)),
		sql.WithForceMigrations(true),
	)
	require.NotNil(t, db)
}
