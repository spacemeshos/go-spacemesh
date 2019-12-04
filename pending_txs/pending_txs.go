package pending_txs

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

type AccountPendingTxs struct {
	PendingTxs map[uint64]map[types.TransactionId]nanoTx // nonce -> TxID -> nanoTx
	mu         sync.RWMutex
}

func NewAccountPendingTxs() *AccountPendingTxs {
	return &AccountPendingTxs{
		PendingTxs: make(map[uint64]map[types.TransactionId]nanoTx),
	}
}

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

func (apt *AccountPendingTxs) Remove(accepted, rejected []*types.Transaction, layer types.LayerID) {
	apt.mu.Lock()
	for _, tx := range accepted {
		delete(apt.PendingTxs, tx.AccountNonce)
	}
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

func (apt *AccountPendingTxs) RemoveNonce(nonce uint64, deleteTx func(id types.TransactionId)) {
	apt.mu.Lock()
	for id := range apt.PendingTxs[nonce] {
		deleteTx(id)
	}
	delete(apt.PendingTxs, nonce)
	apt.mu.Unlock()
}

func (apt *AccountPendingTxs) GetProjection(prevNonce, prevBalance uint64) (nonce, balance uint64) {
	_, nonce, balance = apt.ValidTxs(prevNonce, prevBalance)
	return nonce, balance
}

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

func (apt *AccountPendingTxs) IsEmpty() bool {
	apt.mu.RLock()
	defer apt.mu.RUnlock()
	return len(apt.PendingTxs) == 0
}

func validTxWithHighestFee(txs map[types.TransactionId]nanoTx, balance uint64) types.TransactionId {
	bestId := types.EmptyTransactionId
	var maxFee uint64
	for id, tx := range txs {
		if (tx.Fee > maxFee || (tx.Fee == maxFee && bytes.Compare(id[:], bestId[:]) < 0)) &&
			balance >= (tx.Amount+tx.Fee) {

			maxFee = tx.Fee
			bestId = id
		}
	}
	return bestId
}
