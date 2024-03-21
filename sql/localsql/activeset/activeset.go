package activeset

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type Kind uint8

const (
	Tortoise Kind = iota
	Hare
)

// Add adds an activeset with weight to the database.
// It is expected to return error if activeset is not unique per epoch.
func Add(
	db sql.Executor,
	kind Kind,
	epoch types.EpochID,
	id types.Hash32,
	weight uint64,
	set []types.ATXID,
) error {
	buf := codec.MustEncodeSlice(set)
	if _, err := db.Exec(`
		insert into prepared_activeset (kind, epoch, id, weight, data)
		values (?1, ?2, ?3, ?4, ?5);`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(kind))
			stmt.BindInt64(2, int64(epoch))
			stmt.BindBytes(3, id.Bytes())
			stmt.BindInt64(4, int64(weight))
			stmt.BindBytes(5, buf)
		}, nil,
	); err != nil {
		return fmt.Errorf("failed to save activeset %s for epoch %d: %w", id.ShortString(), epoch, err)
	}
	return nil
}

func Get(db sql.Executor, kind Kind, epoch types.EpochID) (types.Hash32, uint64, []types.ATXID, error) {
	var (
		id     types.Hash32
		weight uint64
		set    []types.ATXID
	)
	rows, err := db.Exec("select id, weight, data from prepared_activeset where kind = ?1 and epoch = ?2;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(kind))
			stmt.BindInt64(2, int64(epoch))
		}, func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, id[:])
			weight = uint64(stmt.ColumnInt64(1))
			set = codec.MustDecodeSliceFromReader[types.ATXID](stmt.ColumnReader(2))
			return true
		})
	if err != nil {
		return id, 0, nil, fmt.Errorf("failed to get activeset %v for epoch %d: %v", kind, epoch, err)
	} else if rows == 0 {
		return id, 0, nil, fmt.Errorf("%w: activeset %v for epoch %d", sql.ErrNotFound, kind, epoch)
	}
	return id, weight, set, nil
}
