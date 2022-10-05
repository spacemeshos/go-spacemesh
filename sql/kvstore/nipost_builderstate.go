package kvstore

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const NIPostBuilderStateKey = "NIPostBuilderState"

// AddNIPostBuilderState adds the data for nipost builder state to the key-value store.
func AddNIPostBuilderState(db sql.Executor, state *types.NIPostBuilderState) error {
	b, err := codec.Encode(state)
	if err != nil {
		return fmt.Errorf("serialize NIPoST builder state: %w", err)
	}

	if _, err := db.Exec(`
		insert into kvstore (id, value) values (?1, ?2)
		on conflict (id) do
		update set value = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, []byte(NIPostBuilderStateKey))
			stmt.BindBytes(2, b)
		}, nil); err != nil {
		return fmt.Errorf("insert NIPoST builder state: %w", err)
	}
	return nil
}

// GetNIPostBuilderState returns the data for nipost builder state from the key-value store.
func GetNIPostBuilderState(db sql.Executor) (*types.NIPostBuilderState, error) {
	var val []byte
	if rows, err := db.Exec("select value from kvstore where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, []byte(NIPostBuilderStateKey))
	}, func(stmt *sql.Statement) bool {
		val = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, val[:])
		return true
	}); err != nil {
		return nil, fmt.Errorf("get NIPoST builder state: %w", err)
	} else if rows == 0 {
		return nil, fmt.Errorf("get NIPoST builder state: %w", sql.ErrNotFound)
	}

	if len(val) == 0 {
		return nil, nil
	}

	res := &types.NIPostBuilderState{}
	if err := codec.Decode(val, res); err != nil {
		return nil, fmt.Errorf("parse NIPoST builder state: %w", err)
	}
	return res, nil
}

// ClearNIPostBuilderState clears the data for nipost builder state from the key-value store.
func ClearNIPostBuilderState(db sql.Executor) error {
	if _, err := db.Exec("delete from kvstore where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, []byte(NIPostBuilderStateKey))
	}, nil); err != nil {
		return fmt.Errorf("clear NIPoST builder state: %w", err)
	}
	return nil
}
