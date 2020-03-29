// Package pendingtxs exposes the AccountPendingTxs type, which keeps track of transactions that haven't been applied to
// global state yet. If also provides projectors that predict an account's nonce and balance after those transactions
// would be applied.
package pendingtxs

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"sync"
)

type nanoTx struct {
	Amount                 uint64
	Fee                    uint64
	HighestLayerIncludedIn types.LayerID
}

// AccountPendingTxs indexes the pending transactions (those that are in the mesh, but haven't been applied yet) of a
// specific account.
type AccountPendingTxs struct {
	PendingTxs map[uint64]map[types.TransactionId]nanoTx // nonce -> TxID -> nanoTx
	mu         sync.RWMutex
}

// NewAccountPendingTxs returns a new, initialized AccountPendingTxs structure.
func NewAccountPendingTxs() *AccountPendingTxs {
	return &AccountPendingTxs{
		PendingTxs: make(map[uint64]map[types.TransactionId]nanoTx),
	}
}

// Add adds transactions to a specific layer. It also updates the highest layer each transaction is included in, if this
// transaction is already indexed.
func (apt *AccountPendingTxs) Add(layer types.LayerID, txs ...*types.Transaction) {
	apt.mu.Lock()
	for _, tx := range txs {
		existing, found := apt.PendingTxs[tx.AccountNonce]
		if !found {
			existing = make(map[types.TransactionId]nanoTx)
			apt.PendingTxs[tx.AccountNonce] = existing
		}
		if existing[tx.Id()].HighestLayerIncludedIn > layer {
			layer = existing[tx.Id()].HighestLayerIncludedIn
		}
		existing[tx.Id()] = nanoTx{
			Amount:                 tx.Amount,
			Fee:                    tx.Fee,
			HighestLayerIncludedIn: layer,
		}
	}
	apt.mu.Unlock()
}

// RemoveAccepted removes a list of accepted transactions from the AccountPendingTxs. Since the given transactions were
// accepted, any other version of the transaction with the same nonce is also discarded.
func (apt *AccountPendingTxs) RemoveAccepted(accepted []*types.Transaction) {
	apt.mu.Lock()
	for _, tx := range accepted {
		delete(apt.PendingTxs, tx.AccountNonce)
	}
	apt.mu.Unlock()
}

// RemoveRejected removes a list of rejected transactions from the AccountPendingTxs, assuming they were rejected in the
// given layer. If any of the listed transactions also appears in a higher layer than the one given, it will not be
// removed.
func (apt *AccountPendingTxs) RemoveRejected(rejected []*types.Transaction, layer types.LayerID) {
	apt.mu.Lock()
	for _, tx := range rejected {
		existing, found := apt.PendingTxs[tx.AccountNonce]
		if found {
			if existing[tx.Id()].HighestLayerIncludedIn > layer {
				continue
			}
			delete(existing, tx.Id())
			if len(existing) == 0 {
				delete(apt.PendingTxs, tx.AccountNonce)
			}
		}
	}
	apt.mu.Unlock()
}

// RemoveNonce removes any transaction with the given nonce from AccountPendingTxs. For each transaction removed it also
// calls the given deleteTx function with the corresponding transaction ID.
func (apt *AccountPendingTxs) RemoveNonce(nonce uint64, deleteTx func(id types.TransactionId)) {
	apt.mu.Lock()
	for id := range apt.PendingTxs[nonce] {
		deleteTx(id)
	}
	delete(apt.PendingTxs, nonce)
	apt.mu.Unlock()
}

// GetProjection provides projected nonce and balance after valid transactions in the AccountPendingTxs would be
// applied. Since determining which transactions are valid depends on the previous nonce and balance, those must be
// provided.
func (apt *AccountPendingTxs) GetProjection(prevNonce, prevBalance uint64) (nonce, balance uint64) {
	_, nonce, balance = apt.ValidTxs(prevNonce, prevBalance)
	return nonce, balance
}

// ValidTxs provides a list of valid transaction IDs that can be applied from the AccountPendingTxs and a final nonce
// and balance if they would be applied. The validity of transactions depends on the previous nonce and balance, so
// those must be provided.
func (apt *AccountPendingTxs) ValidTxs(prevNonce, prevBalance uint64) (txIds []types.TransactionId, nonce, balance uint64) {
	nonce, balance = prevNonce, prevBalance
	apt.mu.RLock()
	for {
		txs, found := apt.PendingTxs[nonce]
		if !found {
			break // no transactions found with required nonce
		}
		id := validTxWithHighestFee(txs, balance)
		if id == types.EmptyTransactionId {
			break // all transactions would overdraft the account
		}
		txIds = append(txIds, id)
		tx := txs[id]
		balance -= tx.Amount + tx.Fee
		nonce++
	}
	apt.mu.RUnlock()
	return txIds, nonce, balance
}

// IsEmpty is true if there are no transactions in this object.
func (apt *AccountPendingTxs) IsEmpty() bool {
	apt.mu.RLock()
	defer apt.mu.RUnlock()
	return len(apt.PendingTxs) == 0
}

func validTxWithHighestFee(txs map[types.TransactionId]nanoTx, balance uint64) types.TransactionId {
	bestID := types.EmptyTransactionId
	var maxFee uint64
	for id, tx := range txs {
		if (tx.Fee > maxFee || (tx.Fee == maxFee && bytes.Compare(id[:], bestID[:]) < 0)) &&
			balance >= (tx.Amount+tx.Fee) {

			maxFee = tx.Fee
			bestID = id
		}
	}
	return bestID
}
