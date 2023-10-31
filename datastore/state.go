package datastore

import "github.com/spacemeshos/go-spacemesh/sql"

type LocalDB struct {
	*sql.Database
}

func NewLocalDB(db *sql.Database) *LocalDB {
	return &LocalDB{Database: db}
}
