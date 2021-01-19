package types

// xdrMarshal is a marshaller interface to encode/decode transaction header
type xdrMarshal interface {
	XdrBytes() ([]byte, error)
	XdrFill([]byte) error
}

// IncompleteCommonTx is a common partial implementation of the IncompleteTransaction
type IncompleteCommonTx struct {
	marshal xdrMarshal
	txType  TransactionType
}

// AuthenticationMessage returns the authentication message for the transaction
func (t IncompleteCommonTx) AuthenticationMessage() (txm TransactionAuthenticationMessage, err error) {
	txm.TransactionData, err = t.marshal.XdrBytes()
	if err != nil {
		return
	}
	txm.TxType = t.txType
	txm.NetID = GetNetworkID()
	return
}

// Type returns transaction's type
func (t IncompleteCommonTx) Type() TransactionType {
	return t.txType
}

// CommonTx is an common partial implementation of the Transaction
type CommonTx struct {
	IncompleteCommonTx
	origin    Address
	id        TransactionID
	signature TxSignature
	pubKey    TxPublicKey
}

// Origin returns transaction's origin, it implements Transaction.Origin
func (tx CommonTx) Origin() Address {
	return tx.origin
}

// Signature returns transaction's signature, it implements Transaction.Signature
func (tx CommonTx) Signature() TxSignature {
	return tx.signature
}

// PubKey returns transaction's public key, it implements Transaction.PubKey
func (tx CommonTx) PubKey() TxPublicKey {
	return tx.pubKey
}

// ID returns the transaction's ID. , it implements Transaction.ID
func (tx *CommonTx) ID() TransactionID {
	return tx.id
}

// Hash32 returns the TransactionID as a Hash32.
func (tx *CommonTx) Hash32() Hash32 {
	return tx.id.Hash32()
}

// ShortString returns a the first 5 characters of the ID, for logging purposes.
func (tx *CommonTx) ShortString() string {
	return tx.id.ShortString()
}

// Encode encodes the transaction to a signed transaction. It implements Transaction.Encode
func (tx *CommonTx) Encode() (_ SignedTransaction, err error) {
	txm, err := tx.AuthenticationMessage()
	if err != nil {
		return
	}
	return txm.Encode(tx.pubKey, tx.signature)
}

// decode decodes fills the transaction object from the transaction bytes
func (tx *CommonTx) decode(marshal xdrMarshal, data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (err error) {
	if err = marshal.XdrFill(data); err != nil {
		return
	}
	tx.txType = txtp
	tx.origin = BytesToAddress(pubKey.Bytes())
	tx.id = txid
	tx.pubKey = pubKey
	tx.signature = signature
	tx.marshal = marshal
	return
}
