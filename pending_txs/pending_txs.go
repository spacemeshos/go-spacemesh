package pending_txs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type nanoTx struct {
	Amount                 uint64
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

func (apt *AccountPendingTxs) Add(txs []types.TinyTx, layer types.LayerID) {
	for _, tx := range txs {
		existing, found := apt.PendingTxs[tx.Nonce]
		if !found {
			existing = make(map[types.TransactionId]nanoTx)
			apt.PendingTxs[tx.Nonce] = existing
		}
		if existing[tx.Id].HighestLayerIncludedIn > layer {
			layer = existing[tx.Id].HighestLayerIncludedIn
		}
		existing[tx.Id] = nanoTx{
			Amount:                 tx.Amount,
			HighestLayerIncludedIn: layer,
		}
	}
}

func (apt *AccountPendingTxs) Remove(accepted []types.TinyTx, rejected []types.TinyTx, layer types.LayerID) {
	for _, tx := range accepted {
		delete(apt.PendingTxs, tx.Nonce)
	}
	for _, tx := range rejected {
		existing, found := apt.PendingTxs[tx.Nonce]
		if found {
			if existing[tx.Id].HighestLayerIncludedIn > layer {
				continue
			}
			delete(existing, tx.Id)
			if len(existing) == 0 {
				delete(apt.PendingTxs, tx.Nonce)
			}
		}
	}
}

func (apt *AccountPendingTxs) RemoveNonce(nonce uint64) {
	delete(apt.PendingTxs, nonce)
}

func (apt *AccountPendingTxs) GetProjection(prevNonce, prevBalance uint64) (nonce, balance uint64) {
	nonce = prevNonce
	balance = prevBalance
	for {
		txs, found := apt.PendingTxs[nonce]
		if !found {
			break
		}
		var maxValidAmount uint64
		for _, tx := range txs {
			if tx.Amount > maxValidAmount && balance >= tx.Amount {
				maxValidAmount = tx.Amount
			}
		}
		if maxValidAmount == 0 { // No transaction can be added without depleting the account
			break
		}
		balance -= maxValidAmount
		nonce++
	}
	return nonce, balance
}

func (apt *AccountPendingTxs) IsEmpty() bool {
	return len(apt.PendingTxs) == 0
}
