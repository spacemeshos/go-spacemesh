package txs

import (
	"bytes"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type nanoTx struct {
	Amount                 uint64
	Fee                    uint64
	HighestLayerIncludedIn types.LayerID
}

// accountPendingTxs indexes the pending transactions (those that are in the mesh, but haven't been applied yet) of a
// specific account.
type accountPendingTxs struct {
	PendingTxs map[uint64]map[types.TransactionID]nanoTx // nonce -> TxID -> nanoTx
	mu         sync.RWMutex
}

// newAccountPendingTxs returns a new, initialized accountPendingTxs structure.
func newAccountPendingTxs() *accountPendingTxs {
	return &accountPendingTxs{
		PendingTxs: make(map[uint64]map[types.TransactionID]nanoTx),
	}
}

// Add adds transactions to a specific layer. It also updates the highest layer each transaction is included in, if this
// transaction is already indexed.
func (apt *accountPendingTxs) Add(layer types.LayerID, txs ...*types.Transaction) {
	apt.mu.Lock()
	defer apt.mu.Unlock()
	for _, tx := range txs {
		existing, found := apt.PendingTxs[tx.AccountNonce]
		if !found {
			existing = make(map[types.TransactionID]nanoTx)
			apt.PendingTxs[tx.AccountNonce] = existing
		}
		if existing[tx.ID()].HighestLayerIncludedIn.After(layer) {
			layer = existing[tx.ID()].HighestLayerIncludedIn
		}
		existing[tx.ID()] = nanoTx{
			Amount:                 tx.Amount,
			Fee:                    tx.GetFee(),
			HighestLayerIncludedIn: layer,
		}
	}
}

// RemoveAccepted removes a list of accepted transactions from the accountPendingTxs. Since the given transactions were
// accepted, any other version of the transaction with the same nonce is also discarded.
func (apt *accountPendingTxs) RemoveAccepted(accepted []*types.Transaction) {
	apt.mu.Lock()
	defer apt.mu.Unlock()
	for _, tx := range accepted {
		delete(apt.PendingTxs, tx.AccountNonce)
	}
}

// RemoveRejected removes a list of rejected transactions from the accountPendingTxs, assuming they were rejected in the
// given layer. If any of the listed transactions also appears in a higher layer than the one given, it will not be
// removed.
func (apt *accountPendingTxs) RemoveRejected(rejected []*types.Transaction, layer types.LayerID) {
	apt.mu.Lock()
	defer apt.mu.Unlock()
	for _, tx := range rejected {
		existing, found := apt.PendingTxs[tx.AccountNonce]
		if found {
			if existing[tx.ID()].HighestLayerIncludedIn.After(layer) {
				continue
			}
			delete(existing, tx.ID())
			if len(existing) == 0 {
				delete(apt.PendingTxs, tx.AccountNonce)
			}
		}
	}
}

// RemoveNonce removes any transaction with the given nonce from accountPendingTxs. For each transaction removed it also
// calls the given deleteTx function with the corresponding transaction ID.
func (apt *accountPendingTxs) RemoveNonce(nonce uint64, deleteTx func(id types.TransactionID)) {
	apt.mu.Lock()
	defer apt.mu.Unlock()
	for id := range apt.PendingTxs[nonce] {
		deleteTx(id)
	}
	delete(apt.PendingTxs, nonce)
}

// GetProjection provides projected nonce and balance after valid transactions in the accountPendingTxs would be
// applied. Since determining which transactions are valid depends on the previous nonce and balance, those must be
// provided.
func (apt *accountPendingTxs) GetProjection(prevNonce, prevBalance uint64) (nonce, balance uint64) {
	_, nonce, balance = apt.ValidTxs(prevNonce, prevBalance)
	return nonce, balance
}

// ValidTxs provides a list of valid transaction IDs that can be applied from the accountPendingTxs and a final nonce
// and balance if they would be applied. The validity of transactions depends on the previous nonce and balance, so
// those must be provided.
func (apt *accountPendingTxs) ValidTxs(prevNonce, prevBalance uint64) (txIds []types.TransactionID, nonce, balance uint64) {
	nonce, balance = prevNonce, prevBalance
	apt.mu.RLock()
	defer apt.mu.RUnlock()
	for {
		txs, found := apt.PendingTxs[nonce]
		if !found {
			break // no transactions found with required nonce
		}
		id := validTxWithHighestFee(txs, balance)
		if id == types.EmptyTransactionID {
			break // all transactions would overdraft the account
		}
		txIds = append(txIds, id)
		tx := txs[id]
		balance -= tx.Amount + tx.Fee
		nonce++
	}
	return txIds, nonce, balance
}

// IsEmpty is true if there are no transactions in this object.
func (apt *accountPendingTxs) IsEmpty() bool {
	apt.mu.RLock()
	defer apt.mu.RUnlock()
	return len(apt.PendingTxs) == 0
}

func validTxWithHighestFee(txs map[types.TransactionID]nanoTx, balance uint64) types.TransactionID {
	bestID := types.EmptyTransactionID
	var maxFee uint64
	for id, tx := range txs {
		if balance >= (tx.Amount+tx.Fee) &&
			(bestID == types.EmptyTransactionID || // first tx with enough balance
				tx.Fee > maxFee ||
				(tx.Fee == maxFee && bytes.Compare(id[:], bestID[:]) < 0)) {
			maxFee = tx.Fee
			bestID = id
		}
	}
	return bestID
}
