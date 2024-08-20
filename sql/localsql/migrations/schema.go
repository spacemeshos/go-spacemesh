package migrations

import (
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func SchemaWithInCodeMigrations() (*sql.Schema, error) {
	return localsql.Schema(
	// add coded migrations here
	)
}
