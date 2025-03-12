package sqlstore

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// CreateDB creates a test database. It is only used for testing.
func CreateDB(t *testing.T, keyLen int) sql.Database {
	db := sql.InMemoryTest(t)
	_, err := db.Exec(
		fmt.Sprintf("create table foo(id char(%d) not null primary key)", keyLen), nil, nil)
	require.NoError(t, err)
	return db
}

func insertDBItems(t *testing.T, db sql.Database, content []rangesync.KeyBytes, cmd string) {
	err := db.WithTx(func(tx sql.Transaction) error {
		for _, id := range content {
			_, err := tx.Exec(cmd, func(stmt *sql.Statement) {
				stmt.BindBytes(1, id)
			}, nil)
			if err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
}

// InsertDBItems inserts items into a test database. It is only used for testing.
func InsertDBItems(t *testing.T, db sql.Database, content []rangesync.KeyBytes) {
	insertDBItems(t, db, content, "insert into foo(id) values(?)")
}

// EnsureDBItems inserts items into a test database, skipping rows with keys that already exist.
// It is only used for testing.
func EnsureDBItems(t *testing.T, db sql.Database, content []rangesync.KeyBytes) {
	insertDBItems(t, db, content, "insert into foo(id) values(?) on conflict do nothing")
}

// PopulateDB creates a test database and inserts items into it. It is only used for testing.
func PopulateDB(t *testing.T, keyLen int, content []rangesync.KeyBytes) sql.Database {
	db := CreateDB(t, keyLen)
	InsertDBItems(t, db, content)
	return db
}
