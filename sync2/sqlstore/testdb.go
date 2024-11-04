package sqlstore

import (
	"context"
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

// InsertDBItems inserts items into a test database. It is only used for testing.
func InsertDBItems(t *testing.T, db sql.Database, content []rangesync.KeyBytes) {
	err := db.WithTx(context.Background(), func(tx sql.Transaction) error {
		for _, id := range content {
			_, err := tx.Exec(
				"insert into foo(id) values(?)",
				func(stmt *sql.Statement) {
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

// PopulateDB creates a test database and inserts items into it. It is only used for testing.
func PopulateDB(t *testing.T, keyLen int, content []rangesync.KeyBytes) sql.Database {
	db := CreateDB(t, keyLen)
	InsertDBItems(t, db, content)
	return db
}
