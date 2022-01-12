package layers

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Status of the layer.
type Status int8

func (s Status) String() string {
	switch s {
	case Latest:
		return "latest"
	case Processed:
		return "processed"
	case Applied:
		return "applied"
	default:
		panic(fmt.Sprintf("unknown status %d", s))
	}
}

const (
	// Latest layer that was added to db.
	Latest Status = iota
	// Processed layer is either synced from peers or updated by hare.
	Processed
	// Applied layer to the state.
	Applied
)

// SetHareOutput for the layer to a block id.
func SetHareOutput(db sql.Executor, lid types.LayerID, output types.BlockID) error {
	if _, err := db.Exec(`insert into layers (id, hare_output) values (?1, ?2) 
					on conflict(id) do update set hare_output=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, output[:])
		}, nil); err != nil {
		return fmt.Errorf("set hare output %s: %w", lid, err)
	}
	return nil
}

// GetHareOutput for layer.
func GetHareOutput(db sql.Executor, lid types.LayerID) (rst types.BlockID, err error) {
	if rows, err := db.Exec("select hare_output from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w hare output for %s is null", sql.ErrNotFound, lid)
				return false
			}
			stmt.ColumnBytes(0, rst[:])
			return true
		}); err != nil {
		return rst, fmt.Errorf("is empty %s: %w", lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w hare output is not set for %s", sql.ErrNotFound, lid)
	}
	return rst, err
}

// SetStatus updates status of the layer.
func SetStatus(db sql.Executor, lid types.LayerID, status Status) error {
	if _, err := db.Exec(`insert into layers (id, status) values (?1, ?2) 
					on conflict(id) do update set status=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindInt64(2, int64(status))
		}, nil); err != nil {
		return fmt.Errorf("insert %s %s: %w", lid, status, err)
	}
	return nil
}

// GetByStatus return latest layer with the status.
func GetByStatus(db sql.Executor, status Status) (rst types.LayerID, err error) {
	if _, err := db.Exec("select max(id) from layers where status >= ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(status))
		},
		func(stmt *sql.Statement) bool {
			rst = types.NewLayerID(uint32(stmt.ColumnInt(0)))
			return true
		}); err != nil {
		return types.LayerID{}, fmt.Errorf("layer by status %s: %w", status, err)
	}
	return rst, nil
}
