package layers

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
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
	if _, err := db.Exec(
		"update layers set applied_block = null, state_hash = null, aggregated_hash = null where id >= ?1;",
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

func FirstAppliedInEpoch(db sql.Executor, epoch types.EpochID) (types.BlockID, error) {
	var (
		result types.BlockID
		err    error
		rows   int
	)
	if rows, err = db.Exec(`
		select applied_block from layers 
		where id between ?1 and ?2 and applied_block is not null and applied_block != ?3
		order by id asc limit 1;`, func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch.FirstLayer()))
		stmt.BindInt64(2, int64((epoch+1).FirstLayer()-1))
		stmt.BindBytes(3, types.EmptyBlockID[:])
	}, func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, result[:])
		return true
	}); err != nil {
		return types.EmptyBlockID, fmt.Errorf("FirstAppliedInEpoch %s: %w", epoch, err)
	} else if rows == 0 {
		return types.EmptyBlockID, fmt.Errorf("FirstAppliedInEpoch %s: %w", epoch, sql.ErrNotFound)
	}
	return result, nil
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

func IterateAggHashes(
	db sql.Executor,
	from, to types.LayerID,
	by uint32,
	callback func(total int, id types.Hash32) error,
) error {
	var (
		err, callbackErr error
		rows             int
	)
	dist := to.Difference(from)
	count := int(dist/by + 1)
	if dist%by != 0 {
		// last layer is not a multiple of By, so we need to add it
		count++
	}
	if rows, err = db.Exec(
		`select (
                  select count(*) from layers
                  where id >= ?1 and id <= ?2 and
			((id-?1)%?3 = 0 or id = ?2)
                ) as total, aggregated_hash from layers
	 	where id >= ?1 and id <= ?2 and
			((id-?1)%?3 = 0 or id = ?2)
		order by id asc;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(from.Uint32()))
			stmt.BindInt64(2, int64(to.Uint32()))
			stmt.BindInt64(3, int64(by))
		},
		func(stmt *sql.Statement) bool {
			var h types.Hash32
			total := stmt.ColumnInt(0)
			stmt.ColumnBytes(1, h[:])
			if total != count {
				callbackErr = fmt.Errorf("%w layers from %s to %s by %d", sql.ErrNotFound, from, to, by)
				return false
			}
			if callbackErr = callback(total, h); callbackErr != nil {
				return false
			}
			return true
		}); err != nil {
		return fmt.Errorf("get aggHashes from %s to %s by %d: %w", from, to, by, err)
	}
	if callbackErr != nil {
		return callbackErr
	}
	if rows != count {
		return fmt.Errorf("%w layers from %s to %s by %d", sql.ErrNotFound, from, to, by)
	}
	return nil
}

func GetAggHashes(db sql.Executor, from, to types.LayerID, by uint32) (hashes []types.Hash32, err error) {
	if err := IterateAggHashes(db, from, to, by, func(total int, id types.Hash32) error {
		if hashes == nil {
			hashes = make([]types.Hash32, 0, total)
		}
		hashes = append(hashes, id)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(hashes) != cap(hashes) {
		panic("BUG: bad aggregate hash count")
	}
	return hashes, nil
}

type Layer struct {
	Id             types.LayerID
	WeakCoin       bool
	Processed      bool
	AppliedBlock   types.BlockID
	StateHash      types.Hash32
	AggregatedHash types.Hash32
	Block          *types.Block
}

func IterateLayersWithBlockOps(
	db sql.Executor,
	operations builder.Operations,
	fn func(layer *Layer) bool,
) error {
	var derr error
	_, err := db.Exec(
		`SELECT l.id, l.weak_coin, l.processed, l.applied_block, l.state_hash, l.aggregated_hash,
       		b.validity, b.block FROM layers l
       		    LEFT JOIN blocks b ON l.id = b.layer`+
			builder.FilterFrom(operations),
		builder.BindingsFrom(operations),
		func(stmt *sql.Statement) bool {
			l := &Layer{
				Id: types.LayerID(stmt.ColumnInt(0)),
			}

			l.WeakCoin = stmt.ColumnInt(1) == 0
			l.Processed = stmt.ColumnInt(2) == 1
			stmt.ColumnBytes(3, l.AppliedBlock[:])
			stmt.ColumnBytes(4, l.StateHash[:])
			stmt.ColumnBytes(5, l.AggregatedHash[:])

			inner := types.InnerBlock{}
			_, err := codec.DecodeFrom(stmt.ColumnReader(7), &inner)
			if err != nil {
				return fn(l)
			}

			l.Block, err = types.NewExistingBlock(l.AppliedBlock, inner), nil
			if err != nil {
				derr = err
				return false
			}

			return fn(l)
		})
	if err != nil {
		return err
	}
	return derr
}

func Get(
	db sql.Executor,
	lid types.LayerID,
) (*Layer, error) {
	var layer *Layer
	var derr error
	_, err := db.Exec(
		`SELECT l.id, l.weak_coin, l.processed, l.applied_block, l.state_hash, l.aggregated_hash,
       		b.validity, b.block FROM layers l
       		    LEFT JOIN blocks b ON l.id = b.layer WHERE l.id = ?1`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Uint32()))
		},
		func(stmt *sql.Statement) bool {
			layer = &Layer{
				Id: types.LayerID(stmt.ColumnInt(0)),
			}

			layer.WeakCoin = stmt.ColumnInt(1) == 0
			layer.Processed = stmt.ColumnInt(2) == 1
			stmt.ColumnBytes(3, layer.AppliedBlock[:])
			stmt.ColumnBytes(4, layer.StateHash[:])
			stmt.ColumnBytes(5, layer.AggregatedHash[:])

			inner := types.InnerBlock{}
			_, err := codec.DecodeFrom(stmt.ColumnReader(7), &inner)
			if err != nil {
				derr = fmt.Errorf("failed to decode block %s: %w", layer.AppliedBlock, err)
				return false
			}

			layer.Block, err = types.NewExistingBlock(layer.AppliedBlock, inner), nil
			if err != nil {
				derr = err
				return false
			}

			return true
		})
	if err != nil {
		return nil, err
	}
	return layer, derr
}
