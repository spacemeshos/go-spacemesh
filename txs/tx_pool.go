package txs

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/codec"
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

// txPool is a struct that holds txs received via gossip network.
type txPool struct {
	db       *sql.Database
	mu       sync.RWMutex
	txs      map[types.TransactionID]*types.Transaction
	accounts map[types.Address]*AccountPendingTxs
}

// newTxPool returns a new txPool struct.
func newTxPool(db *sql.Database) *txPool {
	return &txPool{
		db:       db,
		txs:      make(map[types.TransactionID]*types.Transaction),
		accounts: make(map[types.Address]*AccountPendingTxs),
	}
}

type txFetcher struct {
	t *txPool
}

// Get transaction blob, by transaction id.
func (f *txFetcher) Get(hash []byte) ([]byte, error) {
	id := types.TransactionID{}
	copy(id[:], hash)
	if tx, err := f.t.getFromMemPool(id); err == nil && tx != nil {
		return codec.Encode(tx)
	}
	return transactions.GetBlob(f.t.db, id)
}

func (tp *txPool) getFromMemPool(id types.TransactionID) (*types.MeshTransaction, error) {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	if tx, found := tp.txs[id]; found {
		return &types.MeshTransaction{
			Transaction: *tx,
			State:       types.MEMPOOL,
		}, nil
	}

	return nil, errors.New("tx not in mempool")
}

func (tp *txPool) addToMemPool(id types.TransactionID, tx *types.Transaction) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	tp.txs[id] = tx
	account, found := tp.accounts[tx.Origin()]
	if !found {
		account = NewAccountPendingTxs()
		tp.accounts[tx.Origin()] = account
	}
	account.Add(types.LayerID{}, tx)
}

func (tp *txPool) removeFromMemPool(id types.TransactionID) {
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

func (tp *txPool) getDBProjection(addr types.Address, prevNonce, prevBalance uint64) (uint64, uint64, error) {
	txs, err := transactions.FilterPending(tp.db, addr)
	if err != nil {
		return 0, 0, fmt.Errorf("db get pending txs: %w", err)
	}

	if len(txs) == 0 {
		return prevNonce, prevBalance, nil
	}

	pending := NewAccountPendingTxs()
	for _, tx := range txs {
		pending.Add(tx.LayerID, &tx.Transaction)
	}
	nonce, balance := pending.GetProjection(prevNonce, prevBalance)
	return nonce, balance, nil
}

func (tp *txPool) getMemPoolProjection(addr types.Address, prevNonce, prevBalance uint64) (uint64, uint64) {
	tp.mu.RLock()
	account, found := tp.accounts[addr]
	tp.mu.RUnlock()
	if !found {
		return prevNonce, prevBalance
	}

	return account.GetProjection(prevNonce, prevBalance)
}

func (tp *txPool) getProjection(addr types.Address, getState func(types.Address) (uint64, uint64), mode projectionMode) (uint64, uint64, error) {
	prevNonce, prevBalance := getState(addr)
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

func (tp *txPool) getMemPoolCandidates(numTXs int, getState func(types.Address) (uint64, uint64)) ([]types.TransactionID, []*types.Transaction, error) {
	txIDs := make([]types.TransactionID, 0, numTXs)

	tp.mu.RLock()
	defer tp.mu.RUnlock()
	for addr, account := range tp.accounts {
		nonce, balance, err := tp.getProjection(addr, getState, mesh)
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

func (tp *txPool) getFromDB(id types.TransactionID) (*types.MeshTransaction, error) {
	mtx, err := transactions.Get(tp.db, id)
	if err != nil {
		return nil, errors.New("tx not in db")
	}
	return mtx, nil
}

func (tp *txPool) getFromDBByAddress(from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	return transactions.FilterByAddress(tp.db, from, to, address)
}

func (tp *txPool) markApplied(tid types.TransactionID) error {
	return transactions.Applied(tp.db, tid)
}

func (tp *txPool) markDeleted(tid types.TransactionID) error {
	if err := transactions.MarkDeleted(tp.db, tid); err != nil && !errors.Is(err, sql.ErrNotFound) {
		return err
	}
	return nil
}

// writeForBlock writes all transactions associated with a block atomically.
func (tp *txPool) writeForBlock(layerID types.LayerID, bid types.BlockID, txs ...*types.Transaction) error {
	dbtx, err := tp.db.Tx(context.Background())
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
