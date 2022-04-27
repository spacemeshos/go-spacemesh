package txs

import (
	"context"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

type store struct {
	logger log.Log
	db     *sql.Database
}

func newStore(db *sql.Database) *store {
	return &store{
		db: db,
	}
}

// LastAppliedLayer returns the last layer applied in mesh.
func (s *store) LastAppliedLayer() (types.LayerID, error) {
	// it's not correct to query transactions table for max applied layer because
	// layer can be empty (contains no transactions).
	return layers.GetLastApplied(s.db)
}

// Add adds a transaction to the database.
func (s *store) Add(tx *types.Transaction, received time.Time) error {
	return transactions.Add(s.db, tx, received)
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
	return transactions.GetByAddress(s.db, from, to, address)
}

// DiscardNonceBelow discards pending transactions with nonce lower than `nonce`.
func (s *store) DiscardNonceBelow(addr types.Address, nonce uint64) error {
	return transactions.DiscardNonceBelow(s.db, addr, nonce)
}

// SetNextLayerBlock sets and returns the next applicable layer/block for the transaction.
func (s *store) SetNextLayerBlock(tid types.TransactionID, lid types.LayerID) (types.LayerID, types.BlockID, error) {
	return transactions.SetNextLayer(s.db, tid, lid)
}

// GetAllPending gets all pending transactions for all accounts from database.
func (s *store) GetAllPending() ([]*types.MeshTransaction, error) {
	return transactions.GetAllPending(s.db)
}

// GetAcctPendingFromNonce gets all pending transactions with nonce <= `from` for an account.
func (s *store) GetAcctPendingFromNonce(addr types.Address, from uint64) ([]*types.MeshTransaction, error) {
	return transactions.GetAcctPendingFromNonce(s.db, addr, from)
}

func (s *store) runInDBTransaction(fn func(*sql.Tx) error) error {
	dbtx, err := s.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer dbtx.Release()

	if err = fn(dbtx); err != nil {
		return err
	}
	return dbtx.Commit()
}

// AddToProposal adds a transaction to a proposal in the database.
func (s *store) AddToProposal(lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	return s.runInDBTransaction(func(dbtx *sql.Tx) error {
		return addToProposal(dbtx, lid, pid, tids)
	})
}

func addToProposal(dbtx *sql.Tx, lid types.LayerID, pid types.ProposalID, tids []types.TransactionID) error {
	for _, tid := range tids {
		if err := transactions.AddToProposal(dbtx, tid, lid, pid); err != nil {
			return fmt.Errorf("add2prop %w", err)
		}
		_, err := transactions.UpdateIfBetter(dbtx, tid, lid, types.EmptyBlockID)
		if err != nil {
			return fmt.Errorf("add2prop update %w", err)
		}
	}
	return nil
}

// AddToBlock adds a transaction to a block in the database.
func (s *store) AddToBlock(lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	return s.runInDBTransaction(func(dbtx *sql.Tx) error {
		return addToBlock(dbtx, lid, bid, tids)
	})
}

func addToBlock(dbtx *sql.Tx, lid types.LayerID, bid types.BlockID, tids []types.TransactionID) error {
	for _, tid := range tids {
		if err := transactions.AddToBlock(dbtx, tid, lid, bid); err != nil {
			return fmt.Errorf("add2block %w", err)
		}
		_, err := transactions.UpdateIfBetter(dbtx, tid, lid, bid)
		if err != nil {
			return fmt.Errorf("add2block update %w", err)
		}
	}
	return nil
}

// ApplyLayer sets transactions to applied and discarded accordingly, and sets the layer at which the
// transactions are applied/discarded.
func (s *store) ApplyLayer(lid types.LayerID, bid types.BlockID, addr types.Address, appliedByNonce map[uint64]types.TransactionID) error {
	return s.runInDBTransaction(func(dbtx *sql.Tx) error {
		return applyLayer(dbtx, lid, bid, addr, appliedByNonce)
	})
}

func applyLayer(dbtx *sql.Tx, lid types.LayerID, bid types.BlockID, addr types.Address, appliedByNonce map[uint64]types.TransactionID) error {
	// nonce order doesn't matter here
	for nonce, tid := range appliedByNonce {
		updated, err := transactions.Apply(dbtx, tid, lid, bid)
		if err != nil {
			return fmt.Errorf("apply %w", err)
		}
		if updated == 0 {
			return fmt.Errorf("tx not applied %v", tid)
		}
		if err = transactions.DiscardByAcctNonce(dbtx, tid, lid, addr, nonce); err != nil {
			return fmt.Errorf("apply discard %w", err)
		}
	}
	return nil
}

// UndoLayers resets all transactions that were applied/discarded between `from` and the most recent layer,
// and reset their layers if they were included in a proposal/block.
func (s *store) UndoLayers(from types.LayerID) error {
	return s.runInDBTransaction(func(dbtx *sql.Tx) error {
		return undoLayers(dbtx, from)
	})
}

func undoLayers(dbtx *sql.Tx, from types.LayerID) error {
	tids, err := transactions.UndoLayers(dbtx, from)
	if err != nil {
		return fmt.Errorf("undo %w", err)
	}
	for _, tid := range tids {
		if _, _, err = transactions.SetNextLayer(dbtx, tid, from.Sub(1)); err != nil {
			return fmt.Errorf("reset for undo %w", err)
		}
	}
	return nil
}
