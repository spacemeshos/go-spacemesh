package transactions

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	pending = iota
	applied
	// NOTE(dshulyak) transaction is marked as deleted, instead of deleting it
	// to avoid problems with data availability until we handle it properly.
	deleted
)

// Add pending transaction to the database. If transaction already exists layer and block will be updated.
func Add(db sql.Executor, lid types.LayerID, bid types.BlockID, tx *types.Transaction) error {
	buf, err := codec.Encode(tx)
	if err != nil {
		return fmt.Errorf("encode %+v: %w", tx, err)
	}
	if _, err := db.Exec(`insert into transactions
	(id, tx, layer, block, origin, destination, status)
	values (?1, ?2, ?3, ?4, ?5, ?6, ?7)
	on conflict(id) do
	update set layer = ?3, block=?4, status = ?7`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, tx.ID().Bytes())
			stmt.BindBytes(2, buf)
			stmt.BindInt64(3, int64(lid.Value))
			stmt.BindBytes(4, bid.Bytes())
			stmt.BindBytes(5, tx.Origin().Bytes())
			stmt.BindBytes(6, tx.Recipient.Bytes())
			stmt.BindInt64(7, pending)
		}, nil); err != nil {
		return fmt.Errorf("insert %s: %w", tx.ID(), err)
	}
	return nil
}

func updateStatus(db sql.Executor, id types.TransactionID, status int64) error {
	if rows, err := db.Exec("update transactions set status = ?2 where id = ?1 returning id", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, status)
	}, nil); err != nil {
		return fmt.Errorf("update %s: %w", id, err)
	} else if rows == 0 {
		return fmt.Errorf("%w: tx %s", sql.ErrNotFound, id)
	}
	return nil
}

// Applied update transaction when it is no longer pending.
func Applied(db sql.Executor, id types.TransactionID) error {
	return updateStatus(db, id, applied)
}

// MarkDeleted marks transaction as deleted.
func MarkDeleted(db sql.Executor, id types.TransactionID) error {
	return updateStatus(db, id, deleted)
}

// tx, layer, block, origin.
func decodeTransaction(id types.TransactionID, stmt *sql.Statement) (*types.MeshTransaction, error) {
	var (
		tx     types.Transaction
		origin types.Address
		bid    types.BlockID
	)
	if _, err := codec.DecodeFrom(stmt.ColumnReader(0), &tx); err != nil {
		return nil, fmt.Errorf("decode %w", err)
	}
	lid := types.NewLayerID(uint32(stmt.ColumnInt64(1)))
	stmt.ColumnBytes(2, bid[:])
	stmt.ColumnBytes(3, origin[:])
	tx.SetOrigin(origin)
	tx.SetID(id)
	return &types.MeshTransaction{
		Transaction: tx,
		LayerID:     lid,
		BlockID:     bid,
	}, nil
}

// Get transaction from database.
func Get(db sql.Executor, id types.TransactionID) (tx *types.MeshTransaction, err error) {
	if rows, err := db.Exec("select tx, layer, block, origin from transactions where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			tx, err = decodeTransaction(id, stmt)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: tx %s", sql.ErrNotFound, id)
	}
	return tx, err
}

// GetBlob loads transaction as an encoded blob, ready to be sent over the wire.
func GetBlob(db sql.Executor, id types.TransactionID) (buf []byte, err error) {
	if rows, err := db.Exec("select tx from transactions where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			buf = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: tx %s", sql.ErrNotFound, id)
	}
	return buf, nil
}

// Has returns true if transaction is stored in the database.
func Has(db sql.Executor, id types.TransactionID) (bool, error) {
	rows, err := db.Exec("select 1 from transactions where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil)
	if err != nil {
		return false, fmt.Errorf("has %s: %w", id, err)
	}
	return rows > 0, nil
}

// query MUST ensure that this order of fields tx, layer, block, origin, id.
func filter(db sql.Executor, query string, encoder func(*sql.Statement)) (rst []*types.MeshTransaction, err error) {
	if _, err := db.Exec(query, encoder, func(stmt *sql.Statement) bool {
		var (
			tx *types.MeshTransaction
			id types.TransactionID
		)
		stmt.ColumnBytes(4, id[:])
		tx, err = decodeTransaction(id, stmt)
		if err != nil {
			return false
		}
		rst = append(rst, tx)
		return true
	}); err != nil {
		return nil, fmt.Errorf("query transactions: %w", err)
	}
	return rst, err
}

func filterByAddress(db sql.Executor, addrfield string, from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return filter(db, fmt.Sprintf(`select tx, layer, block, origin, id from transactions
		where %s = ?1 and layer >= ?2 and layer <= ?3 and status != ?4`, addrfield), func(stmt *sql.Statement) {
		stmt.BindBytes(1, address[:])
		stmt.BindInt64(2, int64(from.Value))
		stmt.BindInt64(3, int64(to.Value))
		stmt.BindInt64(4, deleted)
	})
}

const (
	originField      = "origin"
	destinationField = "destination"
)

// FilterByOrigin filter transaction by origin [from, to] layers.
func FilterByOrigin(db sql.Executor, from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return filterByAddress(db, originField, from, to, address)
}

// FilterByDestination filter transaction by destnation [from, to] layers.
func FilterByDestination(db sql.Executor, from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return filterByAddress(db, destinationField, from, to, address)
}

// FilterByAddress finds all transactions for an address.
func FilterByAddress(db sql.Executor, from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return filter(db, `select tx, layer, block, origin, id from transactions
	where origin = ?1 or destination = ?1 and layer >= ?2 and layer <= ?3 and status != ?4`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, address[:])
		stmt.BindInt64(2, int64(from.Value))
		stmt.BindInt64(3, int64(to.Value))
		stmt.BindInt64(4, deleted)
	})
}

// FilterPending filters all transactions that are not yet applied.
func FilterPending(db sql.Executor, address types.Address) ([]*types.MeshTransaction, error) {
	return filter(db, `select tx, layer, block, origin, id from transactions
		where origin = ?1 and status = ?2`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, address[:])
		stmt.BindInt64(2, pending)
	})
}
