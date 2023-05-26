package layers

import (
	"fmt"
	"strings"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// SetWeakCoin for the layer.
func SetWeakCoin(db sql.Executor, lid types.LayerID, weakcoin bool) error {
	if _, err := db.Exec(`insert into layers (id, weak_coin) values (?1, ?2) 
					on conflict(id) do update set weak_coin=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid))
			stmt.BindBool(2, weakcoin)
		}, nil); err != nil {
		return fmt.Errorf("set weak coin %s: %w", lid, err)
	}
	return nil
}

// GetWeakCoin for layer.
func GetWeakCoin(db sql.Executor, lid types.LayerID) (bool, error) {
	var (
		weakcoin bool
		err      error
		rows     int
	)
	if rows, err = db.Exec("select weak_coin from layers where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid))
		},
		func(stmt *sql.Statement) bool {
			if stmt.ColumnLen(0) == 0 {
				err = fmt.Errorf("%w weak coin for %s is null", sql.ErrNotFound, lid)
				return false
			}
			weakcoin = stmt.ColumnInt(0) == 1
			return true
		}); err != nil {
		return false, fmt.Errorf("is empty %s: %w", lid, err)
	} else if rows == 0 {
		return false, fmt.Errorf("%w weak coin is not set for %s", sql.ErrNotFound, lid)
	}
	return weakcoin, err
}

// SetApplied for the layer to a block id.
func SetApplied(db sql.Executor, lid types.LayerID, applied types.BlockID) error {
	if _, err := db.Exec(`insert into layers (id, applied_block) values (?1, ?2) 
					on conflict(id) do update set applied_block=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid))
			stmt.BindBytes(2, applied[:])
		}, nil); err != nil {
		return fmt.Errorf("set applied %s: %w", lid, err)
	}
	return nil
}

// UnsetAppliedFrom updates the applied block to nil for layer >= `lid`.
func UnsetAppliedFrom(db sql.Executor, lid types.LayerID) error {
	if _, err := db.Exec("update layers set applied_block = null, state_hash = null, aggregated_hash = null where id >= ?1;",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid))
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
			stmt.BindInt64(1, int64(lid))
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
			stmt.BindInt64(1, int64(lid))
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
			stmt.BindInt64(1, int64(lid))
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
			lid = types.LayerID(uint32(stmt.ColumnInt64(0)))
			return true
		}); err != nil {
		return lid, fmt.Errorf("last applied: %w", err)
	}
	return lid, nil
}

// SetProcessed sets a layer processed.
func SetProcessed(db sql.Executor, lid types.LayerID) error {
	if _, err := db.Exec(
		`insert into layers (id, processed) values (?1, 1) 
         on conflict(id) do update set processed=1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
		}, nil); err != nil {
		return fmt.Errorf("set processed %v: %w", lid, err)
	}
	return nil
}

// GetProcessed gets the highest layer processed.
func GetProcessed(db sql.Executor) (types.LayerID, error) {
	var lid types.LayerID
	if _, err := db.Exec("select max(id) from layers where processed = 1;",
		nil,
		func(stmt *sql.Statement) bool {
			lid = types.LayerID(uint32(stmt.ColumnInt64(0)))
			return true
		}); err != nil {
		return lid, fmt.Errorf("processed layer: %w", err)
	}
	return lid, nil
}

// SetMeshHash sets the aggregated hash up to the specified layer.
func SetMeshHash(db sql.Executor, lid types.LayerID, aggHash types.Hash32) error {
	if _, err := db.Exec(
		`insert into layers (id, aggregated_hash) values (?1, ?2) 
         on conflict(id) do update set aggregated_hash=?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
			stmt.BindBytes(2, aggHash[:])
		}, nil); err != nil {
		return fmt.Errorf("set hashes %v: %w", lid, err)
	}
	return nil
}

// GetAggregatedHash for layer.
func GetAggregatedHash(db sql.Executor, lid types.LayerID) (types.Hash32, error) {
	var rst types.Hash32
	if rows, err := db.Exec("select aggregated_hash from layers where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
		},
		func(stmt *sql.Statement) bool {
			stmt.ColumnBytes(0, rst[:])
			return true
		}); err != nil {
		return rst, fmt.Errorf("get agg hash %s: %w", lid, err)
	} else if rows == 0 {
		return rst, fmt.Errorf("%w layer %s", sql.ErrNotFound, lid)
	}
	return rst, nil
}

func makeInClause(num int) string {
	var sb strings.Builder
	for i := 0; i < num; i++ {
		sb.WriteString("?")
		if i < num-1 {
			sb.WriteString(",")
		}
	}
	return sb.String()
}

func GetAggHashes(db sql.Executor, lids []types.LayerID) ([]types.Hash32, error) {
	hashes := make([]types.Hash32, 0, len(lids))
	inClause := makeInClause(len(lids))
	if _, err := db.Exec(fmt.Sprintf("select aggregated_hash from layers where id in (%s) order by id asc;", inClause),
		func(stmt *sql.Statement) {
			for i, lid := range lids {
				stmt.BindInt64(i+1, int64(lid.Uint32()))
			}
		},
		func(stmt *sql.Statement) bool {
			var h types.Hash32
			stmt.ColumnBytes(0, h[:])
			hashes = append(hashes, h)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get aggHashes %v: %w", lids, err)
	}
	if len(hashes) != len(lids) {
		return nil, fmt.Errorf("%w layers %s", sql.ErrNotFound, lids)
	}
	return hashes, nil
}
