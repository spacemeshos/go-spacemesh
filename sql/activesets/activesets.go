package activesets

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Add(db sql.Executor, id types.Hash32, set *types.EpochActiveSet) error {
	_, err := db.Exec(`insert into activesets
		(id, epoch, active_set)
		values (?1, ?2, ?3);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id[:])
			stmt.BindInt64(2, int64(set.Epoch))
			stmt.BindBytes(3, codec.MustEncode(set))
		}, nil)
	if err != nil {
		return fmt.Errorf("add active set %v: %w", id.String(), err)
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
		return nil, fmt.Errorf("active set %v: %w", id.String(), sql.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("copy active set %v: %w", id.String(), err)
	}
	if err2 != nil {
		return nil, fmt.Errorf("failed to decode data %v: %w", id.String(), err2)
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
		return nil, fmt.Errorf("active set %x: %w", id, sql.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get active set blob %x: %w", id, err)
	}
	return rst, nil
}

func DeleteBeforeEpoch(db sql.Executor, epoch types.EpochID) error {
	_, err := db.Exec("delete from activesets where epoch < ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("delete activesets before %v: %w", epoch, err)
	}
	return nil
}
