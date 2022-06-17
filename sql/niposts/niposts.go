package niposts

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/sql"
)

// Add the nipost state to the database.
func Add(db sql.Executor, id, data []byte) error {
	if _, err := db.Exec(`
		insert into niposts (id, value) values (?1, ?2)
		on conflict (id) do
		update set value = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id)
			stmt.BindBytes(2, data)
		}, nil); err != nil {
		return fmt.Errorf("insert %s: %w", id, err)
	}
	return nil
}

// Get the nipost state from database.
func Get(db sql.Executor, id []byte) ([]byte, error) {
	var val []byte
	if rows, err := db.Exec("select value from niposts where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id)
	}, func(stmt *sql.Statement) bool {
		val = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, val[:])
		return true
	}); err != nil {
		return nil, fmt.Errorf("get nipost %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w nipost %s", sql.ErrNotFound, id)
	}
	return val, nil
}
