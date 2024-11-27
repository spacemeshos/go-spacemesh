package migrations

import (
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func SchemaWithInCodeMigrations(config config.Config) (*sql.Schema, error) {
	return statesql.Schema(
		New0021Migration(1_000_000),
		New0025Migration(config),
	)
}
