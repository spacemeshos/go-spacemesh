package transactions

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Add unapplied transaction to the database.
func Add(db sql.Executor, lid types.LayerID, tx *types.Transaction) error {
	return nil
}

// Update layer/block when transaction is included into the mesh.
func Update(db sql.Executor, id types.TransactionID, lid types.LayerID, bid types.BlockID) error {
	return nil
}

// Delete transaction from database.
func Delete(db sql.Executor, id types.TransactionID) error {
	return nil
}

// Get transaction from database.
func Get(db sql.Executor, id types.TransactionID) (*types.MeshTransaction, error) {
	return nil, nil
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
