package localsql

import "github.com/spacemeshos/go-spacemesh/sql"

type Database struct {
	*sql.Database
}

func Open(uri string, opts ...sql.Opt) (*Database, error) {
	migrations, err := sql.LocalMigrations()
	if err != nil {
		return nil, err
	}
	defaultOpts := []sql.Opt{
		sql.WithConnections(16),
		sql.WithMigrations(migrations),
	}
	opts = append(defaultOpts, opts...)
	db, err := sql.Open(uri, opts...)
	if err != nil {
		return nil, err
	}
	return &Database{Database: db}, nil
}

func InMemory(opts ...sql.Opt) *Database {
	migrations, err := sql.LocalMigrations()
	if err != nil {
		panic(err)
	}
	defaultOpts := []sql.Opt{
		sql.WithConnections(1),
		sql.WithMigrations(migrations),
	}
	opts = append(defaultOpts, opts...)
	db := sql.InMemory(opts...)
	return &Database{Database: db}
}
