package txs

import (
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

type projectionMode uint

const (
	mesh projectionMode = iota
	meshAndMempool
)

// TxPool is a struct that holds txs received via gossip network.
type TxPool struct {
	db       *sql.Database
	mu       sync.RWMutex
	txs      map[types.TransactionID]*types.Transaction
	accounts map[types.Address]*AccountPendingTxs
}

// NewTxPool returns a new TxMempool struct.
func NewTxPool(db *sql.Database) *TxPool {
	return &TxPool{
		db:       db,
		txs:      make(map[types.TransactionID]*types.Transaction),
		accounts: make(map[types.Address]*AccountPendingTxs),
	}
}

// Get returns transaction by provided id, it returns an error if transaction is not found.
func (tp *TxPool) Get(id types.TransactionID) (*types.Transaction, error) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if tx, found := tp.txs[id]; found {
		return tx, nil
	}
	return nil, errors.New("transaction not found in mempool")
}

// Put inserts a transaction into the mem pool. It indexes it by source and dest addresses as well.
func (tp *TxPool) Put(id types.TransactionID, tx *types.Transaction) {
	tp.put(id, tx)
	events.ReportNewTx(types.LayerID{}, tx)
	events.ReportAccountUpdate(tx.Origin())
	events.ReportAccountUpdate(tx.GetRecipient())
}

func (tp *TxPool) put(id types.TransactionID, tx *types.Transaction) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.txs[id] = tx
	tp.getOrCreate(tx.Origin()).Add(types.LayerID{}, tx)
}

// Invalidate removes transaction from pool.
func (tp *TxPool) Invalidate(id types.TransactionID) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if tx, found := tp.txs[id]; found {
		if pendingTxs, found := tp.accounts[tx.Origin()]; found {
			// Once a tx appears in a block we want to invalidate all of this nonce's variants. The mempool currently
			// only accepts one version, but this future-proofs it.
			pendingTxs.RemoveNonce(tx.AccountNonce, func(id types.TransactionID) {
				delete(tp.txs, id)
			})
			if pendingTxs.IsEmpty() {
				delete(tp.accounts, tx.Origin())
			}
			// We only report those transactions that are being dropped from the txpool here as
			// conflicting since they won't be reported anywhere else. There is no need to report
			// the initial tx here since it'll be reported as part of a new block/layer anyway.
			events.ReportTxWithValidity(types.LayerID{}, tx, false)
		}
	}
}

func (tp *TxPool) getDBProjection(addr types.Address, prevNonce, prevBalance uint64) (uint64, uint64, error) {
	txs, err := transactions.FilterPending(tp.db, addr)
	if err != nil {
		return 0, 0, fmt.Errorf("db get pending txs: %w", err)
	}

	pending := NewAccountPendingTxs()
	for _, tx := range txs {
		pending.Add(tx.LayerID, &tx.Transaction)
	}
	nonce, balance := pending.GetProjection(prevNonce, prevBalance)
	return nonce, balance, nil
}

func (tp *TxPool) getMemPoolProjection(addr types.Address, prevNonce, prevBalance uint64) (uint64, uint64) {
	tp.mu.RLock()
	account, found := tp.accounts[addr]
	tp.mu.RUnlock()
	if !found {
		return prevNonce, prevBalance
	}

	return account.GetProjection(prevNonce, prevBalance)
}

// GetProjection returns the estimated nonce and balance for the provided address addr and previous nonce and balance
// projecting state is done by applying transactions from the pool.
func (tp *TxPool) GetProjection(addr types.Address, prevNonce, prevBalance uint64, mode projectionMode) (uint64, uint64, error) {
	nonce, balance, err := tp.getDBProjection(addr, prevNonce, prevBalance)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get db projection: %w", err)
	}
	if mode == mesh {
		return nonce, balance, err
	}
	nonce, balance = tp.getMemPoolProjection(addr, nonce, balance)
	return nonce, balance, nil
}

// ⚠️ must be called under write-lock.
func (tp *TxPool) getOrCreate(addr types.Address) *AccountPendingTxs {
	account, found := tp.accounts[addr]
	if !found {
		account = NewAccountPendingTxs()
		tp.accounts[addr] = account
	}
	return account
}

func (tp *TxPool) getMempoolCandidateTXs(numTXs int, getState func(types.Address) (uint64, uint64)) ([]types.TransactionID, []*types.Transaction, error) {
	txIDs := make([]types.TransactionID, 0, numTXs)

	tp.mu.RLock()
	defer tp.mu.RUnlock()
	for addr, account := range tp.accounts {
		nonce, balance := getState(addr)
		nonce, balance, err := tp.GetProjection(addr, nonce, balance, mesh)
		if err != nil {
			return nil, nil, fmt.Errorf("get projection for addr %s: %v", addr.Short(), err)
		}
		accountTxIds, _, _ := account.ValidTxs(nonce, balance)
		txIDs = append(txIDs, accountTxIds...)
	}

	txs := make([]*types.Transaction, 0, len(txIDs))
	for _, tx := range txIDs {
		txs = append(txs, tp.txs[tx])
	}
	return txIDs, txs, nil
}
