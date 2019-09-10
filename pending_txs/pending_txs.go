package pending_txs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type nanoTx struct {
	Amount                 uint64
	Fee                    uint64
	HighestLayerIncludedIn types.LayerID
}

type AccountPendingTxs struct {
	PendingTxs map[uint64]map[types.TransactionId]nanoTx
}

func NewAccountPendingTxs() *AccountPendingTxs {
	return &AccountPendingTxs{
		PendingTxs: make(map[uint64]map[types.TransactionId]nanoTx),
	}
}

func (apt *AccountPendingTxs) Add(layer types.LayerID, txs ...*types.Transaction) {
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
			Fee:                    tx.GasPrice, // TODO: Filthy hack!
			HighestLayerIncludedIn: layer,
		}
	}
}

func (apt *AccountPendingTxs) Remove(accepted, rejected []*types.Transaction, layer types.LayerID) {
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
}

func (apt *AccountPendingTxs) RemoveNonce(nonce uint64, deleteTx func(id types.TransactionId)) {
	for id := range apt.PendingTxs[nonce] {
		deleteTx(id)
	}
	delete(apt.PendingTxs, nonce)
}

func (apt *AccountPendingTxs) GetProjection(prevNonce, prevBalance uint64) (nonce, balance uint64) {
	_, nonce, balance = apt.ValidTxs(prevNonce, prevBalance)
	return nonce, balance
}

func (apt *AccountPendingTxs) ValidTxs(prevNonce, prevBalance uint64) (txIds []types.TransactionId, nonce, balance uint64) {
	nonce, balance = prevNonce, prevBalance
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
		balance -= txs[id].Amount + txs[id].Fee
		nonce++
	}
	return txIds, nonce, balance
}

func (apt *AccountPendingTxs) IsEmpty() bool {
	return len(apt.PendingTxs) == 0
}

func validTxWithHighestFee(txs map[types.TransactionId]nanoTx, balance uint64) types.TransactionId {
	bestId := types.EmptyTransactionId
	var maxFee uint64
	for id, tx := range txs {
		if tx.Fee > maxFee && balance >= (tx.Amount+tx.Fee) {
			maxFee = tx.Fee
			bestId = id
		}
	}
	return bestId
}
