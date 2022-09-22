package store

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const CommitmentATXKey = "CommitmentATX"

// AddCommitmentATX adds the id for the commitment atx to the key-value store.
func AddCommitmentATX(db sql.Executor, atx types.ATXID) error {
	if _, err := db.Exec(`
		insert into state (id, value) values (?1, ?2)
		on conflict (id) do
		update set value = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, []byte(CommitmentATXKey))
			stmt.BindBytes(2, atx.Bytes())
		}, nil); err != nil {
		return fmt.Errorf("insert CommitmentATX: %w", err)
	}
	return nil
}

// GetCommitmentATX returns the id for the commitment atx from the key-value store.
func GetCommitmentATX(db sql.Executor) (types.ATXID, error) {
	var res types.ATXID
	if rows, err := db.Exec("select value from state where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, []byte(CommitmentATXKey))
	}, func(stmt *sql.Statement) bool {
		val := make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, val[:])
		res = types.ATXID(types.BytesToHash(val))
		return true
	}); err != nil {
		return *types.EmptyATXID, fmt.Errorf("get CommitmentATX: %w", err)
	} else if rows == 0 {
		return *types.EmptyATXID, fmt.Errorf("get CommitmentATX: %w", sql.ErrNotFound)
	}
	return res, nil
}

func ClearCommitmentAtx(db sql.Executor) error {
	if _, err := db.Exec("delete from state where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, []byte(CommitmentATXKey))
	}, nil); err != nil {
		return fmt.Errorf("clear CommitmentATX: %w", err)
	}
	return nil
}
