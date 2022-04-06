package txs

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

type store struct {
	logger log.Log
	db     *sql.Database
}

func newStore(db *sql.Database, logger log.Log) *store {
	return &store{
		logger: logger,
		db:     db,
	}
}

// LastAppliedLayer gets the last applied layer in the database.
func (s *store) LastAppliedLayer() (types.LayerID, error) {
	return types.LayerID{}, nil
}

// Add adds a transaction to the database.
func (s *store) Add(tx *types.Transaction, received time.Time) error {
	return nil
}

// Has returns true if a transaction already exists in the database.
func (s *store) Has(tid types.TransactionID) (bool, error) {
	return transactions.Has(s.db, tid)
}

// Get returns a transaction from the database.
func (s *store) Get(tid types.TransactionID) (*types.MeshTransaction, error) {
	return transactions.Get(s.db, tid)
}

// GetBlob returns a transaction as a byte array.
func (s *store) GetBlob(tid types.TransactionID) ([]byte, error) {
	return transactions.GetBlob(s.db, tid)
}

// GetByAddress returns a list of transactions from `address` with layers in [from, to].
func (s *store) GetByAddress(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.FilterByAddress(s.db, from, to, address)
}

// AddToProposal adds a transaction to a proposal in the database.
func (s *store) AddToProposal(lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	return nil
}

// AddToBlock adds a transaction to a block in the database.
func (s *store) AddToBlock(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	return nil
}

// ApplyLayer sets transactions to applied and discarded accordingly, and sets the layer at which the
// transactions are applied/discarded.
func (s *store) ApplyLayer(lid types.LayerID, bid types.BlockID, addr types.Address, appliedByNonce map[uint64]types.TransactionID) error {
	return nil
}

// DiscardNonceBelow discards pending transactions with nonce lower than `nonce`.
func (s *store) DiscardNonceBelow(addr types.Address, nonce uint64) error {
	return nil
}

// UndoLayers resets all transactions that were applied/discarded between `from` and the most recent layer,
// and reset their layers if they were included in a proposal/block.
func (s *store) UndoLayers(from types.LayerID) error {
	return nil
}

// SetNextLayerBlock sets and returns the next applicable layer/block for the transaction.
func (s *store) SetNextLayerBlock(tid types.TransactionID, lid types.LayerID) (types.LayerID, types.BlockID, error) {
	return types.LayerID{}, types.EmptyBlockID, nil
}

// GetAllPending gets all pending transactions for all accounts from database.
func (s *store) GetAllPending() ([]*types.MeshTransaction, error) {
	return nil, nil
}

// GetAcctPendingFromNonce gets all pending transactions with nonce <= `from` for an account.
func (s *store) GetAcctPendingFromNonce(addr types.Address, from uint64) ([]*types.MeshTransaction, error) {
	return nil, nil
}
