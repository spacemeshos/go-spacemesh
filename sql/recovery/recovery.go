package recovery

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// SetCheckpoint for the node.
func SetCheckpoint(db sql.Executor, restore types.LayerID) error {
	if _, err := db.Exec(`insert into recovery (restore) values (?1);`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(restore))
		}, nil); err != nil {
		return fmt.Errorf("set recovery %s: %w", restore, err)
	}
	return nil
}

// CheckpointInfo for the node.
func CheckpointInfo(db sql.Executor) (types.LayerID, error) {
	var restore types.LayerID

	if rows, err := db.Exec("select restore from recovery;", nil,
		func(stmt *sql.Statement) bool {
			restore = types.LayerID(stmt.ColumnInt64(0))
			return true
		}); err != nil {
		return 0, fmt.Errorf("recovery info: %w", err)
	} else if rows == 0 {
		return 0, nil
	} else if rows > 1 {
		return 0, fmt.Errorf("multiple recovery info: %d", rows)
	}
	return restore, nil
}
