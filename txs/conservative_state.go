package txs

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

var (
	errBadNonce            = errors.New("bad nonce")
	errInsufficientBalance = errors.New("insufficient balance")
)

// ConservativeState provides the conservative version of the SVM state by taking into accounts of
// nonce and balances for pending transactions in un-applied blocks and mempool.
type ConservativeState struct {
	svmState

	logger log.Log
	db     *sql.Database
	pool   *txPool
}

// NewConservativeState returns a ConservativeState.
func NewConservativeState(state svmState, db *sql.Database, logger log.Log) *ConservativeState {
	return &ConservativeState{
		svmState: state,
		db:       db,
		pool:     newTxPool(),
		logger:   logger,
	}
}

func (cs *ConservativeState) getState(addr types.Address) (uint64, uint64) {
	return cs.svmState.GetNonce(addr), cs.svmState.GetBalance(addr)
}

// SelectTXsForProposal picks a specific number of random txs for miner to pack in a proposal.
func (cs *ConservativeState) SelectTXsForProposal(numOfTxs int) ([]types.TransactionID, []*types.Transaction, error) {
	return cs.pool.getCandidates(numOfTxs, cs.getMeshProjection)
}

// GetProjection returns the projected nonce and balance for the address with pending transactions
// in un-applied blocks and mempool.
func (cs *ConservativeState) GetProjection(addr types.Address) (uint64, uint64, error) {
	nonce, balance, err := cs.getMeshProjection(addr)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get db projection: %w", err)
	}
	nonce, balance = cs.pool.getProjection(addr, nonce, balance)
	return nonce, balance, nil
}

func (cs *ConservativeState) getMeshProjection(addr types.Address) (uint64, uint64, error) {
	txs, err := transactions.FilterPending(cs.db, addr)
	if err != nil {
		return 0, 0, fmt.Errorf("db get pending txs: %w", err)
	}

	prevNonce, prevBalance := cs.getState(addr)
	if len(txs) == 0 {
		return prevNonce, prevBalance, nil
	}

	pending := newAccountPendingTxs()
	for _, tx := range txs {
		pending.Add(tx.LayerID, &tx.Transaction)
	}
	nonce, balance := pending.GetProjection(prevNonce, prevBalance)
	return nonce, balance, nil
}

func (cs *ConservativeState) validateNonceAndBalance(tx *types.Transaction) error {
	origin := tx.Origin()
	nonce, balance, err := cs.GetProjection(origin)
	if err != nil {
		return fmt.Errorf("failed to project state for account %v: %v", origin.Short(), err)
	}
	if tx.AccountNonce != nonce {
		return fmt.Errorf("%w: expected: %d, actual: %d", errBadNonce, nonce, tx.AccountNonce)
	}
	if (tx.Amount + tx.GetFee()) > balance { // TODO: Fee represents the absolute fee here, as a temporarily hack
		return fmt.Errorf("%w: available: %d, want to spend: %d[amount]+%d[fee]=%d",
			errInsufficientBalance, balance, tx.Amount, tx.GetFee(), tx.Amount+tx.GetFee())
	}
	return nil
}

// AddTxToMemPool adds the provided transaction to the mempool after checking nonce and balance.
func (cs *ConservativeState) AddTxToMemPool(tx *types.Transaction, checkValidity bool) error {
	if checkValidity {
		// brand new TX
		if err := cs.validateNonceAndBalance(tx); err != nil {
			return err
		}
		events.ReportNewTx(types.LayerID{}, tx)
		events.ReportAccountUpdate(tx.Origin())
		events.ReportAccountUpdate(tx.GetRecipient())
	} else if err := cs.markDeleted(tx.ID()); err != nil {
		return err
	}
	cs.pool.add(tx.ID(), tx)
	return nil
}

// StoreTransactionsFromMemPool takes declared txs from provided proposal and writes them to DB and invalidates
// the transactions from the mempool.
func (cs *ConservativeState) StoreTransactionsFromMemPool(layerID types.LayerID, blockID types.BlockID, txIDs []types.TransactionID) error {
	if len(txIDs) == 0 {
		return nil
	}
	txs := make([]*types.Transaction, 0, len(txIDs))
	for _, txID := range txIDs {
		tx, err := cs.GetMeshTransaction(txID)
		if err != nil {
			return fmt.Errorf("get tx from mem/db: %w", err)
		}
		txs = append(txs, &tx.Transaction)
	}
	if err := cs.writeForBlock(layerID, blockID, txs...); err != nil {
		return fmt.Errorf("write tx: %w", err)
	}

	// remove txs from pool
	for _, id := range txIDs {
		cs.pool.remove(id)
	}
	return nil
}

// ReinsertTxsToMemPool reinserts transactions into mempool.
func (cs *ConservativeState) ReinsertTxsToMemPool(ids []types.TransactionID) error {
	for _, id := range ids {
		if tx, err := cs.GetMeshTransaction(id); err != nil {
			cs.logger.With().Error("failed to find tx", id)
		} else if tx.State != types.MEMPOOL {
			if err = cs.AddTxToMemPool(&tx.Transaction, false); err == nil {
				// We ignore errors here, since they mean that the tx is no longer
				// valid and we shouldn't re-add it.
				cs.logger.With().Debug("tx reinserted to mempool", tx.ID())
			}
		}
	}
	return nil
}

// GetMeshTransaction retrieves a tx by its id.
func (cs *ConservativeState) GetMeshTransaction(id types.TransactionID) (*types.MeshTransaction, error) {
	tx, err := cs.pool.get(id)
	if err == nil {
		return tx, nil
	}

	tx, err = transactions.Get(cs.db, id)
	if err != nil {
		return nil, errors.New("tx not in db")
	}
	return tx, nil
}

// GetTransactions retrieves a list of txs by their id's.
func (cs *ConservativeState) GetTransactions(ids []types.TransactionID) ([]*types.Transaction, map[types.TransactionID]struct{}) {
	missing := make(map[types.TransactionID]struct{})
	txs := make([]*types.Transaction, 0, len(ids))
	for _, tid := range ids {
		var (
			mtx *types.MeshTransaction
			err error
		)
		if mtx, err = cs.GetMeshTransaction(tid); err != nil {
			cs.logger.With().Warning("could not get tx", tid, log.Err(err))
			missing[tid] = struct{}{}
		} else {
			txs = append(txs, &mtx.Transaction)
		}
	}
	return txs, missing
}

// GetTransactionsByAddress retrieves txs for a single address in between layers [from, to].
// Guarantees that transaction will appear exactly once, even if origin and recipient is the same, and in insertion order.
func (cs *ConservativeState) GetTransactionsByAddress(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.FilterByAddress(cs.db, from, to, address)
}

// ApplyLayer applies the transactions specified by the ids to the state.
func (cs *ConservativeState) ApplyLayer(lid types.LayerID, bid types.BlockID, txIDs []types.TransactionID, rewardByMiner map[types.Address]uint64) ([]*types.Transaction, error) {
	txs, missing := cs.GetTransactions(txIDs)
	if len(missing) > 0 {
		return nil, fmt.Errorf("find txs %v for applying layer %v", missing, lid)
	}
	// TODO: should miner IDs be sorted in a deterministic order prior to applying rewards?
	failedTxs, svmErr := cs.svmState.ApplyLayer(lid, txs, rewardByMiner)
	if svmErr != nil {
		cs.logger.With().Error("failed to apply txs",
			lid,
			log.Int("num_failed_txs", len(failedTxs)),
			log.Err(svmErr))
		// TODO: We want to panic here once we have a way to "remember" that we didn't apply these txs
		//  e.g. persist the last layer transactions were applied from and use that instead of `oldVerified`
		return failedTxs, fmt.Errorf("apply layer: %w", svmErr)
	}

	if err := cs.writeForBlock(lid, bid, txs...); err != nil {
		cs.logger.With().Error("failed to update tx block ID in db", log.Err(err))
		return nil, err
	}
	for _, tx := range txs {
		if err := cs.markApplied(tx.ID()); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (cs *ConservativeState) markApplied(tid types.TransactionID) error {
	return transactions.Applied(cs.db, tid)
}

func (cs *ConservativeState) markDeleted(tid types.TransactionID) error {
	if err := transactions.MarkDeleted(cs.db, tid); err != nil && !errors.Is(err, sql.ErrNotFound) {
		return err
	}
	return nil
}

// writeForBlock writes all transactions associated with a block atomically.
func (cs *ConservativeState) writeForBlock(layerID types.LayerID, bid types.BlockID, txs ...*types.Transaction) error {
	dbtx, err := cs.db.Tx(context.Background())
	if err != nil {
		return err
	}
	defer dbtx.Release()
	for _, tx := range txs {
		if err := transactions.Add(dbtx, layerID, bid, tx); err != nil {
			return err
		}
	}
	return dbtx.Commit()
}

// Transactions exports the transactions DB.
func (cs *ConservativeState) Transactions() database.Getter {
	return &txFetcher{
		pool: cs.pool,
		db:   cs.db,
	}
}

type txFetcher struct {
	pool *txPool
	db   *sql.Database
}

// Get transaction blob, by transaction id.
func (f *txFetcher) Get(hash []byte) ([]byte, error) {
	id := types.TransactionID{}
	copy(id[:], hash)
	if tx, err := f.pool.get(id); err == nil && tx != nil {
		return codec.Encode(tx)
	}
	return transactions.GetBlob(f.db, id)
}
