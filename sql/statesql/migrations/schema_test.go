package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestCodedMigrations(t *testing.T) {
	schema, err := statesql.Schema(
		New0021Migration(1_000_000),
		New0025Migration(nil),
	)
	require.NoError(t, err)

	db := sql.InMemory(
		sql.WithDatabaseSchema(schema),
		sql.WithLogger(zaptest.NewLogger(t)),
		sql.WithForceMigrations(true),
	)
	require.NotNil(t, db)
}
