package migrations

import (
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func SchemaWithInCodeMigrations(malHandler malfeasanceValidator) (*sql.Schema, error) {
	return statesql.Schema(
		New0021Migration(1_000_000),
		New0025Migration(malHandler),
	)
}
