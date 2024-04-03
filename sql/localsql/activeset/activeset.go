package activeset

import (
	"bytes"
	"fmt"
	"math"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type Kind uint8

const (
	Tortoise Kind = iota
	Hare
)

// disable verification for max number of atxs as it is performed before calling functions in this module.
const maxAtxs = math.MaxUint32

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
	var b bytes.Buffer
	_, err := scale.EncodeStructSlice(scale.NewEncoder(&b, scale.WithEncodeMaxElements(maxAtxs)), set)
	if err != nil {
		panic(fmt.Sprintf("failed to encode activeset %s for epoch %d: %v", id.ShortString(), epoch, err))
	}
	if _, err := db.Exec(`
		insert into prepared_activeset (kind, epoch, id, weight, data)
		values (?1, ?2, ?3, ?4, ?5);`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(kind))
			stmt.BindInt64(2, int64(epoch))
			stmt.BindBytes(3, id.Bytes())
			stmt.BindInt64(4, int64(weight))
			stmt.BindBytes(5, b.Bytes())
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
			v, _, err := scale.DecodeStructSlice[types.ATXID](
				scale.NewDecoder(stmt.ColumnReader(2), scale.WithDecodeMaxElements(maxAtxs)),
			)
			if err != nil {
				panic(fmt.Sprintf("failed to decode activeset %v for epoch %d: %v", kind, epoch, err))
			}
			set = v
			return true
		})
	if err != nil {
		return id, 0, nil, fmt.Errorf("failed to get activeset %v for epoch %d: %v", kind, epoch, err)
	} else if rows == 0 {
		return id, 0, nil, fmt.Errorf("%w: activeset %v for epoch %d", sql.ErrNotFound, kind, epoch)
	}
	return id, weight, set, nil
}
