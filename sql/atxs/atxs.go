package atxs

import (
	"fmt"
	"io"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Get gets an ATX by a given ATX ID.
func Get(db sql.Executor, id types.ATXID) (atx *types.VerifiedActivationTx, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		var v types.ActivationTx
		if _, decodeErr := codec.DecodeFrom(stmt.ColumnReader(0), &v); decodeErr != nil {
			atx, err = nil, fmt.Errorf("decode %w", decodeErr)
			return true
		}
		v.SetID(&id)
		nodeID := types.NodeID{}
		stmt.ColumnBytes(3, nodeID[:])
		v.SetNodeID(&nodeID)

		baseTickHeight := uint64(stmt.ColumnInt64(1))
		tickCount := uint64(stmt.ColumnInt64(2))
		atx, err = v.Verify(baseTickHeight, tickCount)
		return err == nil
	}

	if rows, err := db.Exec("select atx, base_tick_height, tick_count, smesher from atxs where id = ?1;", enc, dec); err != nil {
		return nil, fmt.Errorf("exec id %v: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("exec id %v: %w", id, sql.ErrNotFound)
	}

	return atx, err
}

// Has checks if an ATX exists by a given ATX ID.
func Has(db sql.Executor, id types.ATXID) (bool, error) {
	rows, err := db.Exec("select 1 from atxs where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil,
	)
	if err != nil {
		return false, fmt.Errorf("exec id %v: %w", id, err)
	}
	return rows > 0, nil
}

// GetTimestamp gets an ATX timestamp by a given ATX ID.
func GetTimestamp(db sql.Executor, id types.ATXID) (timestamp time.Time, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		timestamp = time.Unix(0, stmt.ColumnInt64(0))
		return true
	}

	if rows, err := db.Exec("select timestamp from atxs where id = ?1;", enc, dec); err != nil {
		return time.Time{}, fmt.Errorf("exec id %v: %w", id, err)
	} else if rows == 0 {
		return time.Time{}, fmt.Errorf("exec id %s: %w", id, sql.ErrNotFound)
	}

	return timestamp, err
}

// GetFirstIDByNodeID gets the initial ATX ID for a given node ID.
func GetFirstIDByNodeID(db sql.Executor, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs 
		where smesher = ?1
		order by epoch asc
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec nodeID %v: %w", nodeID, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, sql.ErrNotFound)
	}

	return id, err
}

// GetLastIDByNodeID gets the last ATX ID for a given node ID.
func GetLastIDByNodeID(db sql.Executor, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs 
		where smesher = ?1
		order by epoch desc, timestamp desc
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec nodeID %v: %w", nodeID, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, sql.ErrNotFound)
	}

	return id, err
}

// GetIDByEpochAndNodeID gets an ATX ID for a given epoch and node ID.
func GetIDByEpochAndNodeID(db sql.Executor, epoch types.EpochID, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs 
		where epoch = ?1 and smesher = ?2 
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec nodeID %v: %w", nodeID, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec nodeID %s: %w", nodeID, sql.ErrNotFound)
	}

	return id, err
}

// GetByEpochAndNodeID gets any ATX by the specified NodeID in the given epoch.
func GetByEpochAndNodeID(db sql.Executor, epoch types.EpochID, nodeID types.NodeID) (*types.VerifiedActivationTx, error) {
	var (
		atx *types.VerifiedActivationTx
		err error
	)
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		var (
			v  types.ActivationTx
			id types.ATXID
			n  int
		)
		stmt.ColumnBytes(0, id[:])
		if n, err = codec.DecodeFrom(stmt.ColumnReader(1), &v); err != nil {
			if err != io.EOF {
				err = fmt.Errorf("atx decode epoch %v nodeID %v: %w", epoch, nodeID, err)
				return false
			}
		} else if n == 0 {
			err = fmt.Errorf("atx data missing epoch %v nodeID %v", epoch, nodeID)
			return false
		}
		v.SetID(&id)
		v.SetNodeID(&nodeID)
		baseTickHeight := uint64(stmt.ColumnInt64(2))
		tickCount := uint64(stmt.ColumnInt64(3))
		atx, err = v.Verify(baseTickHeight, tickCount)
		return err == nil
	}

	if rows, err := db.Exec(`
		select id, atx, base_tick_height, tick_count from atxs
		where epoch = ?1 and smesher = ?2
		limit 1;`, enc, dec); err != nil {
		return nil, fmt.Errorf("atx by epoch %v nodeID %v: %w", epoch, nodeID, err)
	} else if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return atx, err
}

// GetIDsByEpoch gets ATX IDs for a given epoch.
func GetIDsByEpoch(db sql.Executor, epoch types.EpochID) (ids []types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
	}
	dec := func(stmt *sql.Statement) bool {
		var id types.ATXID
		stmt.ColumnBytes(0, id[:])
		ids = append(ids, id)
		return true
	}

	if rows, err := db.Exec("select id from atxs where epoch = ?1;", enc, dec); err != nil {
		return nil, fmt.Errorf("exec epoch %v: %w", epoch, err)
	} else if rows == 0 {
		return []types.ATXID{}, nil
	}

	return ids, nil
}

// GetBlob loads ATX as an encoded blob, ready to be sent over the wire.
func GetBlob(db sql.Executor, id []byte) (buf []byte, err error) {
	if rows, err := db.Exec("select atx from atxs where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id)
		}, func(stmt *sql.Statement) bool {
			buf = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", types.BytesToHash(id), err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: atx %s", sql.ErrNotFound, types.BytesToHash(id))
	}
	return buf, nil
}

// Add adds an ATX for a given ATX ID.
func Add(db sql.Executor, atx *types.VerifiedActivationTx, timestamp time.Time) error {
	buf, err := codec.Encode(atx.ActivationTx)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.ID().Bytes())
		stmt.BindInt64(2, int64(atx.PubLayerID.Uint32()))
		stmt.BindInt64(3, int64(atx.PublishEpoch()))
		stmt.BindBytes(4, atx.NodeID().Bytes())
		stmt.BindBytes(5, buf)
		stmt.BindInt64(6, timestamp.UnixNano())
		stmt.BindInt64(7, int64(atx.BaseTickHeight()))
		stmt.BindInt64(8, int64(atx.TickCount()))
	}

	_, err = db.Exec(`
		insert into atxs (id, layer, epoch, smesher, atx, timestamp, base_tick_height, tick_count) 
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8);`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert ATX ID %v: %w", atx.ID(), err)
	}
	return nil
}

// GetAtxIDWithMaxHeight returns the ID of the atx from the last epoch with the highest (or tied for the highest) tick height.
func GetAtxIDWithMaxHeight(db sql.Executor) (types.ATXID, error) {
	var (
		rst types.ATXID
		max uint64
	)
	dec := func(stmt *sql.Statement) bool {
		var id types.ATXID
		stmt.ColumnBytes(0, id[:])
		height := uint64(stmt.ColumnInt64(1)) + uint64(stmt.ColumnInt64(2))
		if height >= max {
			max = height
			rst = id
		}
		return true
	}

	if rows, err := db.Exec("select id, base_tick_height, tick_count, epoch from atxs where epoch = (select max(epoch) from atxs);", nil, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("select positioning atx: %w", err)
	} else if rows == 0 {
		return types.ATXID{}, sql.ErrNotFound
	}
	return rst, nil
}
