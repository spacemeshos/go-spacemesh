package types

type TinyTx struct {
	Id          TransactionId
	Origin      Address
	Nonce       uint64
	TotalAmount uint64
}

func AddressableTxToTiny(tx *AddressableSignedTransaction) TinyTx {
	id := GetTransactionId(tx.SerializableSignedTransaction)
	return TinyTx{
		Id:          id,
		Origin:      tx.Address,
		Nonce:       tx.AccountNonce,
		TotalAmount: tx.Amount + tx.GasPrice, // TODO: GasPrice represents the absolute fee here, as a temporarily hack
	}
}
