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

func (s *store) add(tx *types.Transaction, received time.Time) error {
	return nil
}

func (s *store) has(tid types.TransactionID) bool {
	has, err := transactions.Has(s.db, tid)
	return err == nil && has
}

func (s *store) get(tid types.TransactionID) (*types.MeshTransaction, error) {
	return transactions.Get(s.db, tid)
}

func (s *store) getBlob(tid types.TransactionID) ([]byte, error) {
	return transactions.GetBlob(s.db, tid)
}

func (s *store) getByAddress(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.FilterByAddress(s.db, from, to, address)
}

// addToProposal adds a transaction to a proposal in the database.
func (s *store) addToProposal(lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	return nil
}

// addToBlock adds a transaction to a block in the database.
func (s *store) addToBlock(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	return nil
}

// applyLayer sets transactions to applied and discarded accordingly, and sets the layer at which the
// transactions are applied/discarded.
func (s *store) applyLayer(lid types.LayerID, bid types.BlockID, toApply, toDiscard []types.TransactionID) error {
	return nil
}

// undoApply resets all transactions that were applied/discarded between `from` and the most recent layer.
func (s *store) undoApply(from types.LayerID) error {
	return nil
}

// discard4Ever marks the transaction discarded in the database without setting layer. as a result,
// these transactions will not be reset when state is reverted.
// this should be called on transactions that are rejected due to bad nonce or insufficient balance.
func (s *store) discard4Ever(tid types.TransactionID) error {
	return nil
}

// setNextLayerBlock sets and returns the next applicable layer/block for the transaction.
func (s *store) setNextLayerBlock(tid types.TransactionID, lid types.LayerID) (types.LayerID, types.BlockID, error) {
	return types.LayerID{}, types.EmptyBlockID, nil
}

// getAllPending gets all pending transactions for all accounts from database.
func (s *store) getAllPending() ([]*types.MeshTransaction, error) {
	return nil, nil
}

// getAcctPending gets all pending transactions for an account from database.
func (s *store) getAcctPending(address types.Address) ([]*types.MeshTransaction, error) {
	return nil, nil
}
