package layers

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	hashField           = "hash"
	aggregatedHashField = "aggregated_hash"
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

// SetApplied for the layer to a block id.
func SetApplied(db sql.Executor, lid types.LayerID, applied types.BlockID) error {
	if _, err := db.Exec(`insert into layers (id, applied_block) values (?1, ?2) 
					on conflict(id) do update set applied_block=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, applied[:])
		}, nil); err != nil {
		return fmt.Errorf("set applied %s: %w", lid, err)
	}
	return nil
}

// UnsetAppliedFrom updates the applied block to nil for layer >= `lid`.
func UnsetAppliedFrom(db sql.Executor, lid types.LayerID) error {
	if _, err := db.Exec("update layers set applied_block = null, state_hash = null where id >= ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		}, nil); err != nil {
		return fmt.Errorf("unset applied %s: %w", lid, err)
	}
	return nil
}

// UpdateStateHash for the layer.
func UpdateStateHash(db sql.Executor, lid types.LayerID, hash types.Hash32) error {
	if _, err := db.Exec(`insert into layers (id, state_hash) values (?1, ?2) 
	on conflict(id) do update set state_hash=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, hash[:])
		}, nil); err != nil {
		return fmt.Errorf("set applied %s: %w", lid, err)
	}
	return nil
}

// GetLatestStateHash loads latest state hash.
func GetLatestStateHash(db sql.Executor) (rst types.Hash32, err error) {
	if rows, err := db.Exec("select state_hash from layers where state_hash is not null;",
		nil,
		func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, rst[:])
			return false
		}); err != nil {
		return rst, fmt.Errorf("failed to load latest state root %w", err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w: state root doesnt exist", sql.ErrNotFound)
	}
	return rst, err
}

// GetStateHash loads state hash for the layer.
func GetStateHash(db sql.Executor, lid types.LayerID) (rst types.Hash32, err error) {
	if rows, err := db.Exec("select state_hash from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w: state_hash for %s is not set", sql.ErrNotFound, lid)
				return false
			}
			stmt.ColumnBytes(0, rst[:])
			return false
		}); err != nil {
		return rst, fmt.Errorf("failed to load state root for %v: %w", lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w: %s doesnt exist", sql.ErrNotFound, lid)
	}
	return rst, err
}

// GetApplied for the applied block for layer.
func GetApplied(db sql.Executor, lid types.LayerID) (rst types.BlockID, err error) {
	if rows, err := db.Exec("select applied_block from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w applied for %s is null", sql.ErrNotFound, lid)
				return false
			}
			stmt.ColumnBytes(0, rst[:])
			return true
		}); err != nil {
		return rst, fmt.Errorf("is empty %s: %w", lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w applied is not set for %s", sql.ErrNotFound, lid)
	}
	return rst, err
}

// GetLastApplied for the applied block for layer.
func GetLastApplied(db sql.Executor) (types.LayerID, error) {
	var lid types.LayerID
	if _, err := db.Exec("select max(id) from layers where applied_block is not null", nil,
		func(stmt *sql.Statement) bool {
			lid = types.NewLayerID(uint32(stmt.ColumnInt64(0)))
			return true
		}); err != nil {
		return lid, fmt.Errorf("last applied: %w", err)
	}
	return lid, nil
}

// SetStatus updates status of the layer.
func SetStatus(db sql.Executor, lid types.LayerID, status Status) error {
	if _, err := db.Exec(`insert into mesh_status (layer, status) values (?1, ?2) 
					on conflict(status) do update set layer=max(?1, layer);`,
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
	if _, err := db.Exec("select layer from mesh_status where status = ?1;",
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

func setHash(db sql.Executor, field string, lid types.LayerID, hash types.Hash32) error {
	if _, err := db.Exec(fmt.Sprintf(`insert into layers (id, %[1]s) values (?1, ?2) 
	on conflict(id) do update set %[1]s=?2;`, field),
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
			stmt.BindBytes(2, hash[:])
		}, nil); err != nil {
		return fmt.Errorf("update %s to %s: %w", field, hash, err)
	}
	return nil
}

// SetHash updates hash for layer.
func SetHash(db sql.Executor, lid types.LayerID, hash types.Hash32) error {
	return setHash(db, hashField, lid, hash)
}

// SetAggregatedHash updates aggregated hash for layer.
func SetAggregatedHash(db sql.Executor, lid types.LayerID, hash types.Hash32) error {
	return setHash(db, aggregatedHashField, lid, hash)
}

func getHash(db sql.Executor, field string, lid types.LayerID) (rst types.Hash32, err error) {
	if rows, err := db.Exec(fmt.Sprintf("select %s from layers where id = ?1", field),
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
		},
		func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, rst[:])
			return true
		}); err != nil {
		return rst, fmt.Errorf("select %s for %s: %w", field, lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w layer %s", sql.ErrNotFound, lid)
	}
	return rst, err
}

// GetHash for layer.
func GetHash(db sql.Executor, lid types.LayerID) (types.Hash32, error) {
	return getHash(db, hashField, lid)
}

// GetAggregatedHash for layer.
func GetAggregatedHash(db sql.Executor, lid types.LayerID) (types.Hash32, error) {
	return getHash(db, aggregatedHashField, lid)
}
