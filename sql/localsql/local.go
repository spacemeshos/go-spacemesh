package localsql

import "github.com/spacemeshos/go-spacemesh/sql"

type Database struct {
	*sql.Database
}

var defaultOpts = []sql.Opt{
	sql.WithConnections(16),
	sql.WithMigrations(sql.LocalMigrations),
}

func Open(uri string, opts ...sql.Opt) (*Database, error) {
	opts = append(defaultOpts, opts...)
	db, err := sql.Open(uri, opts...)
	if err != nil {
		return nil, err
	}
	return &Database{Database: db}, nil
}

func InMemory(opts ...sql.Opt) *Database {
	opts = append(opts,
		sql.WithConnections(1),
		sql.WithMigrations(sql.LocalMigrations),
	)
	db := sql.InMemory(opts...)
	return &Database{Database: db}
}
