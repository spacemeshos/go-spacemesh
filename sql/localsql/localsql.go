package localsql

import (
	"embed"
	"strings"
	"testing"

	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:generate go run ../schemagen -dbtype local -output schema/schema.sql

//go:embed schema/schema.sql
var schemaScript string

//go:embed schema/migrations/*.sql
var migrations embed.FS

type database struct {
	sql.Database
}

var _ sql.LocalDatabase = &database{}

func (d *database) IsLocalDatabase() {}

// Schema returns the schema for the local database.
func Schema() (*sql.Schema, error) {
	sqlMigrations, err := sql.LoadSQLMigrations(migrations)
	if err != nil {
		return nil, err
	}
	// NOTE: coded state migrations can be added here
	// They can be a part of this localsql package
	return &sql.Schema{
		Script:     strings.ReplaceAll(schemaScript, "\r", ""),
		Migrations: sqlMigrations,
	}, nil
}

// Open opens a local database.
func Open(uri string, opts ...sql.Opt) (*database, error) {
	schema, err := Schema()
	if err != nil {
		return nil, err
	}
	defaultOpts := []sql.Opt{
		sql.WithConnections(16),
		sql.WithDatabaseSchema(schema),
	}
	opts = append(defaultOpts, opts...)
	db, err := sql.Open(uri, opts...)
	if err != nil {
		return nil, err
	}
	return &database{Database: db}, nil
}

// Open opens an in-memory local database.
func InMemory(opts ...sql.Opt) *database {
	schema, err := Schema()
	if err != nil {
		panic(err)
	}
	defaultOpts := []sql.Opt{
		sql.WithConnections(1),
		sql.WithDatabaseSchema(schema),
	}
	opts = append(defaultOpts, opts...)
	db := sql.InMemory(opts...)
	return &database{Database: db}
}

// InMemoryTest returns an in-mem database for testing and ensures database is closed during `tb.Cleanup`.
func InMemoryTest(tb testing.TB, opts ...sql.Opt) sql.LocalDatabase {
	db := InMemory(opts...)
	tb.Cleanup(func() { db.Close() })
	return db
}
