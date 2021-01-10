package types

type xdrMarshal interface {
	XdrBytes() ([]byte, error)
	XdrFill([]byte) error
}

type IncompleteCommonTx struct {
	marshal xdrMarshal
	txType  TransactionType
}

func (t IncompleteCommonTx) AuthenticationMessage() (txm TransactionAuthenticationMessage, err error) {
	txm.TransactionData, err = t.marshal.XdrBytes()
	if err != nil {
		return
	}
	txm.TxType = t.txType
	txm.NetID = GetNetworkID()
	return
}

func (t IncompleteCommonTx) Type() TransactionType {
	return t.txType
}

type CommonTx struct {
	IncompleteCommonTx
	origin    *Address
	id        *TransactionID
	signature TxSignature
	pubKey    TxPublicKey
}

func (t CommonTx) Origin() Address {
	if t.origin == nil {
		panic("origin not set")
	}
	return *t.origin
}

func (t CommonTx) Signature() TxSignature {
	return t.signature
}

func (t CommonTx) PubKey() TxPublicKey {
	return t.pubKey
}

// ID returns the transaction's ID. If it's not cached, it's calculated, cached and returned.
func (t *CommonTx) ID() TransactionID {
	if t.id == nil {
		panic("transaction id is not set")
	}
	return *t.id
}

// Hash32 returns the TransactionID as a Hash32.
func (t *CommonTx) Hash32() Hash32 {
	return t.ID().Hash32()
}

// ShortString returns a the first 5 characters of the ID, for logging purposes.
func (t *CommonTx) ShortString() string {
	return t.ID().ShortString()
}

func (t *CommonTx) Encode() (_ SignedTransaction, err error) {
	txm, err := t.AuthenticationMessage()
	if err != nil {
		return
	}
	return txm.Encode(t.pubKey, t.signature)
}

func (tx *CommonTx) decode(marshal xdrMarshal, data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (err error) {
	if err = marshal.XdrFill(data); err != nil {
		return
	}
	a := BytesToAddress(pubKey.Bytes())
	tx.txType = txtp
	tx.origin = &a
	tx.id = &txid
	tx.pubKey = pubKey
	tx.signature = signature
	tx.marshal = marshal
	return
}
