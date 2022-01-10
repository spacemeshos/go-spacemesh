package layers

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Status of the layer.
type Status int

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
	Processed = iota
	// Applied layer to the state.
	Applied
)

// SetEmpty marks that layer is empty.
func SetEmpty(db sql.Executor, lid types.LayerID) error {
	if _, err := db.Exec(`insert into layers (id, empty) values (?1, ?2) 
					on conflict(id) do update set empty=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBool(2, true)
		}, nil); err != nil {
		return fmt.Errorf("insert %s: %w", lid, err)
	}
	return nil
}

// IsEmpty checks if the layer is empty.
func IsEmpty(db sql.Executor, lid types.LayerID) (rst bool, err error) {
	_, err = db.Exec("select empty from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			rst = stmt.ColumnInt(0) == 1
			return true
		})
	if err != nil {
		return false, fmt.Errorf("is empty %s: %w", lid, err)
	}
	return rst, nil
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
	_, err = db.Exec("select max(id) from layers where status >= ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(status))
		},
		func(stmt *sql.Statement) bool {
			rst = types.NewLayerID(uint32(stmt.ColumnInt(0)))
			return true
		})
	if err != nil {
		return types.LayerID{}, fmt.Errorf("layer by status %s: %w", status, err)
	}
	return rst, nil
}
