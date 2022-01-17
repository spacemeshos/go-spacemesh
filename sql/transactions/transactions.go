package transactions

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Add unapplied transaction to the database.
func Add(db sql.Executor, lid types.LayerID, bid types.BlockID, tx *types.Transaction) error {
	buf, err := codec.Encode(tx)
	if err != nil {
		return fmt.Errorf("encode %+v: %w", tx, err)
	}
	if _, err := db.Exec(`insert into transactions 
	(id, tx, layer, block, origin, destination) 
	values (?1, ?2, ?3, ?4, ?5, ?6) 
	on conflict(id) 
	update set layer = ?3, block=?4`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, tx.ID().Bytes())
			stmt.BindBytes(2, buf)
			stmt.BindInt64(3, int64(lid.Value))
			stmt.BindBytes(4, bid.Bytes())
			stmt.BindBytes(5, tx.Origin().Bytes())
			stmt.BindBytes(6, tx.Recipient.Bytes())
		}, nil); err != nil {
		return fmt.Errorf("insert %s: %w", tx.ID(), err)
	}
	return nil
}

// Applied update transaction when it is no longer pending.
func Applied(db sql.Executor, id types.TransactionID) error {
	if rows, err := db.Exec("update transactions set applied = 1 where id = ?1 returning id", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, nil); err != nil {
		return fmt.Errorf("applied %s: %w", id, err)
	} else if rows == 0 {
		return fmt.Errorf("%w: tx %s", sql.ErrNotFound, id)
	}
	return nil
}

// Delete transaction from database.
func Delete(db sql.Executor, id types.TransactionID) error {
	if _, err := db.Exec("delete from transactions where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil); err != nil {
		return fmt.Errorf("delete %s: %w", id, err)
	}
	return nil
}

// Get transaction from database.
func Get(db sql.Executor, id types.TransactionID) (tx *types.MeshTransaction, err error) {
	if rows, err := db.Exec("select id, tx, layer, block, origin, destination",
		func(stmt *sql.Statement) {
		}, func(stmt *sql.Statement) bool {
			return true
		}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: tx %s", sql.ErrNotFound, id)
	}
	return tx, err
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

// FilterByOrigin filter transaction by origin [from, to] layers.
func FilterByOrigin(db sql.Executor, from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return nil, nil
}

// FilterByDestination filter transaction by destnation [from, to] layers.
func FilterByDestination(db sql.Executor, from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return nil, nil
}

// FilterPending filters all transactions that are not yet applied (have empty block).
func FilterPending(db sql.Executor, address types.Address) ([]*types.MeshTransaction, error) {
	return nil, nil
}
