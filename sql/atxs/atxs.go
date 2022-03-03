package atxs

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Get gets an ATX by a given ATX ID.
func Get(db sql.Executor, id types.ATXID) (atx *types.ActivationTx, err error) {
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
		atx, err = &v, nil
		return true
	}

	if rows, err := db.Exec("select atx from atxs where id = ?1;", enc, dec); err != nil {
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

// GetLastIDByNodeID gets the last ATX ID for a given node ID.
func GetLastIDByNodeID(db sql.Executor, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.ToBytes())
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
		return types.ATXID{}, fmt.Errorf("exec id %v: %w", id, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec id %s: %w", id, sql.ErrNotFound)
	}

	return id, err
}

// GetIDByEpochAndNodeID gets an ATX ID for a given epoch and node ID.
func GetIDByEpochAndNodeID(db sql.Executor, epoch types.EpochID, nodeID types.NodeID) (id types.ATXID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, nodeID.ToBytes())
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec(`
		select id from atxs 
		where epoch = ?1 and smesher = ?2 
		limit 1;`, enc, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec id %v: %w", id, err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec id %s: %w", id, sql.ErrNotFound)
	}

	return id, err
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
func GetBlob(db sql.Executor, id types.ATXID) (buf []byte, err error) {
	if rows, err := db.Exec("select atx from atxs where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			buf = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: atx %s", sql.ErrNotFound, id)
	}
	return buf, nil
}

// Add adds an ATX for a given ATX ID.
func Add(db sql.Executor, atx *types.ActivationTx, timestamp time.Time) error {
	buf, err := codec.Encode(atx)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.ID().Bytes())
		stmt.BindInt64(2, int64(atx.PubLayerID.Uint32()))
		stmt.BindInt64(3, int64(atx.PubLayerID.GetEpoch()))
		stmt.BindBytes(4, atx.NodeID.ToBytes())
		stmt.BindBytes(5, buf)
		stmt.BindInt64(6, timestamp.UnixNano())
	}

	_, err = db.Exec(`
		insert into atxs (id, layer, epoch, smesher, atx, timestamp) 
		values (?1, ?2, ?3, ?4, ?5, ?6);`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert ATX ID %v: %w", atx.ID(), err)
	}

	return nil
}

// GetTop gets a top ATX (positioning ATX candidate) for a given ATX ID.
// The top ATX is the one with the highest layer ID.
func GetTop(db sql.Executor) (id types.ATXID, err error) {
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, id[:])
		return true
	}

	if rows, err := db.Exec("select atx_id from atx_top;", nil, dec); err != nil {
		return types.ATXID{}, fmt.Errorf("exec: %w", err)
	} else if rows == 0 {
		return types.ATXID{}, fmt.Errorf("exec: %w", sql.ErrNotFound)
	}

	return id, err
}

// UpdateTopIfNeeded updates top ATX if needed.
func UpdateTopIfNeeded(db sql.Executor, atx *types.ActivationTx) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, atx.ID().Bytes())
		stmt.BindInt64(2, int64(atx.PubLayerID.Uint32()))
	}

	_, err := db.Exec(`
		insert into atx_top (id, atx_id, layer) 
		values (1, ?1, ?2)
		on conflict (id) do
		update set 
			layer = case
				when excluded.layer > layer
				then excluded.layer
				else layer
			end,
			atx_id = case
				when excluded.layer > layer
				then excluded.atx_id
				else atx_id
			end;`, enc, nil)
	if err != nil {
		return fmt.Errorf("update top ATX ID %v: %w", atx.ID(), err)
	}

	return nil
}

// DeleteATXsByNodeID deletes ATXs by node ID.
func DeleteATXsByNodeID(db sql.Executor, nodeID types.NodeID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.ToBytes())
	}

	if _, err := db.Exec("delete from atxs where smesher = ?1;", enc, nil); err != nil {
		return fmt.Errorf("exec %v: %w", nodeID, err)
	}

	return nil
}
