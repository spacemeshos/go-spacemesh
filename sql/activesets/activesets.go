package activesets

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Add(db sql.Executor, id types.Hash32, set *types.EpochActiveSet) error {
	buf, err := codec.Encode(set)
	if err != nil {
		return fmt.Errorf("encode active set %s: %w", id, err)
	}
	_, err = db.Exec(`insert into activesets
		(id, active_set)
		values (?1, ?2);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id[:])
			stmt.BindBytes(2, buf)
		}, nil)
	if err != nil {
		return fmt.Errorf("insert active set %s: %w", id, err)
	}
	return nil
}

func Get(db sql.Executor, id types.Hash32) (*types.EpochActiveSet, error) {
	var (
		rst  types.EpochActiveSet
		err2 error
	)
	rows, err := db.Exec("select active_set from activesets where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		},
		func(stmt *sql.Statement) bool {
			_, err2 = codec.DecodeFrom(stmt.ColumnReader(0), &rst)
			return true
		},
	)
	if rows == 0 {
		return nil, fmt.Errorf("active set %s: %w", id, sql.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("copy active set %s: %w", id, err)
	}
	if err2 != nil {
		return nil, fmt.Errorf("failed to decode data %s: %w", id, err2)
	}
	return &rst, nil
}

func GetBlob(db sql.Executor, id []byte) ([]byte, error) {
	var rst []byte
	rows, err := db.Exec("select active_set from activesets where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id)
		},
		func(stmt *sql.Statement) bool {
			rd := stmt.ColumnReader(0)
			rst = make([]byte, rd.Len())
			rd.Read(rst)
			return true
		},
	)
	if rows == 0 {
		return nil, fmt.Errorf("active set %s: %w", id, sql.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get active set blob %s: %w", id, err)
	}
	return rst, nil
}
