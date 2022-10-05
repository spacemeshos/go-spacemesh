package kvstore

import (
	"fmt"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func addKeyValue(db sql.Executor, key string, value scale.Encodable) error {
	bytes, err := codec.Encode(value)
	if err != nil {
		return fmt.Errorf("failed serialization: %w", err)
	}

	if _, err := db.Exec(`
		insert into kvstore (id, value) values (?1, ?2)
		on conflict (id) do
		update set value = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, []byte(key))
			stmt.BindBytes(2, bytes)
		}, nil); err != nil {
		return fmt.Errorf("failed to insert: %w", err)
	}

	return nil
}

func getKeyValue(db sql.Executor, key string, value scale.Decodable) error {
	var val []byte
	if rows, err := db.Exec("select value from kvstore where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, []byte(key))
	}, func(stmt *sql.Statement) bool {
		val = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, val[:])
		return true
	}); err != nil {
		return fmt.Errorf("get value: %w", err)
	} else if rows == 0 {
		return fmt.Errorf("get value: %w", sql.ErrNotFound)
	}

	if len(val) == 0 {
		return nil
	}

	if err := codec.Decode(val, value); err != nil {
		return fmt.Errorf("parse value: %w", err)
	}
	return nil
}

func clearKeyValue(db sql.Executor, key string) error {
	if _, err := db.Exec("delete from kvstore where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, []byte(key))
	}, nil); err != nil {
		return fmt.Errorf("clear value: %w", err)
	}
	return nil
}
