package statesql

import (
	"embed"

	"github.com/spacemeshos/go-spacemesh/sql"
)

//go:embed schema/schema.sql
var schemaScript string

//go:embed schema/migrations/*.sql
var migrations embed.FS

// Database represents a state database.
type Database struct {
	*sql.Database
}

// Schema returns the schema for the state database.
func Schema() (*sql.Schema, error) {
	sqlMigrations, err := sql.LoadSQLMigrations(migrations)
	if err != nil {
		return nil, err
	}
	// NOTE: coded state migrations can be added here
	// They can be a part of this localsql package
	return &sql.Schema{Script: schemaScript, Migrations: sqlMigrations}, nil
}

// Open opens a state database.
func Open(uri string, opts ...sql.Opt) (*Database, error) {
	schema, err := Schema()
	if err != nil {
		return nil, err
	}
	opts = append([]sql.Opt{sql.WithDatabaseSchema(schema)}, opts...)
	db, err := sql.Open(uri, opts...)
	if err != nil {
		return nil, err
	}
	return &Database{Database: db}, nil
}

// Open opens an in-memory state database.
func InMemory(opts ...sql.Opt) *Database {
	schema, err := Schema()
	if err != nil {
		panic(err)
	}
	defaultOpts := []sql.Opt{
		sql.WithDatabaseSchema(schema),
	}
	opts = append(defaultOpts, opts...)
	db := sql.InMemory(opts...)
	return &Database{Database: db}
}
