package activesets

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	CacheKindActiveSetBlob sql.QueryCacheKind = "activeset-blob"
)

func Add(db sql.Executor, id types.Hash32, set *types.EpochActiveSet) error {
	_, err := db.Exec(`insert into activesets
		(id, epoch, active_set)
		values (?1, ?2, ?3);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
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

// GetBlobSizes returns the sizes of the blobs corresponding to activesets with specified
// ids. For non-existent activesets, the corresponding items are set to -1.
func GetBlobSizes(db sql.Executor, ids [][]byte) (sizes []int, err error) {
	return sql.GetBlobSizes(db, "select id, length(active_set) from activesets where id in", ids)
}

// LoadBlob loads activesets as an encoded blob, ready to be sent over the wire.
func LoadBlob(ctx context.Context, db sql.Executor, id []byte, blob *sql.Blob) error {
	if sql.IsCached(db) {
		b, err := getBlob(ctx, db, id)
		if err != nil {
			return err
		}
		blob.Bytes = b
		return nil
	}
	return sql.LoadBlob(db, "select active_set from activesets where id = ?1", id, blob)
}

func getBlob(ctx context.Context, db sql.Executor, id []byte) ([]byte, error) {
	cacheKey := sql.QueryCacheKey(CacheKindActiveSetBlob, string(id))
	return sql.WithCachedValue(ctx, db, cacheKey, func(context.Context) ([]byte, error) {
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
		if err != nil {
			return nil, fmt.Errorf("get active set blob %x: %w", id, err)
		}
		if rows == 0 {
			return nil, fmt.Errorf("active set %x: %w", id, sql.ErrNotFound)
		}
		return rst, nil
	})
}

func DeleteBeforeEpoch(db sql.Executor, epoch types.EpochID) error {
	_, err := db.Exec("delete from activesets where epoch < ?1;",
		func(stmt *sql.Statement) { stmt.BindInt64(1, int64(epoch)) },
		nil,
	)
	if err != nil {
		return fmt.Errorf("delete activesets before %v: %w", epoch, err)
	}
	return nil
}

func Has(db sql.Executor, id types.Hash32) (bool, error) {
	rows, err := db.Exec("select 1 from activesets where id = ?1;",
		func(stmt *sql.Statement) { stmt.BindBytes(1, id.Bytes()) },
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("has activeset %s: %w", id, err)
	}
	return rows > 0, nil
}
