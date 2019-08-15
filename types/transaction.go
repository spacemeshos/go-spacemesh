package types

import (
	"github.com/spacemeshos/go-spacemesh/address"
)

type TinyTx struct {
	Id     TransactionId
	Origin address.Address
	Nonce  uint64
	Amount uint64
	//Fee    uint64
}

func AddressableTxToTiny(tx *AddressableSignedTransaction) TinyTx {
	// TODO: calculate and store fee amount
	id := GetTransactionId(tx.SerializableSignedTransaction)
	return TinyTx{
		Id:     id,
		Origin: tx.Address,
		Nonce:  tx.AccountNonce,
		Amount: tx.Amount,
	}
}
