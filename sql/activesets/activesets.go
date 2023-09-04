package activesets

import (
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Add(db sql.Executor, id types.Hash32, set []types.ATXID) error {
	buf, err := codec.EncodeSlice(set)
	if err != nil {
		return fmt.Errorf("encode active set %s: %w", id, err)
	}
	if _, err := db.Exec(`insert into activesets
		(id, set)
		values (?1, ?2);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id[:])
			stmt.BindBytes(2, buf)
		}, nil); err != nil {
		return fmt.Errorf("insert active set %s: %w", id, err)
	}
	return nil
}

func Copy(db sql.Executor, id types.Hash32, dst io.Writer) error {
	var ioerr error
	rows, err := db.Exec("select buf from activesets where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		},
		func(stmt *sql.Statement) bool {
			_, ioerr = io.Copy(dst, stmt.ColumnReader(0))
			return true
		},
	)
	if rows == 0 {
		return fmt.Errorf("active set %s: %w", id, sql.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("copy active set %s: %w", id, err)
	}
	if ioerr != nil {
		return fmt.Errorf("failed to copy %s: %w", id, err)
	}
	return nil
}
