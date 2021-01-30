package types

import (
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

// txSelf is an interface to encode/decode transaction header or access to self Transaction interface
type txSelf interface {
	xdrBytes() ([]byte, error)
	xdrFill([]byte) (int, error)
	complete() *CommonTx
}

type txMutable interface {
	immutableBytes() ([]byte, error)
}

// IncompleteCommonTx is a common partial implementation of the IncompleteTransaction
type IncompleteCommonTx struct {
	self   txSelf
	txType TransactionType
}

// Digest calculates message digest
func (t IncompleteCommonTx) Digest() (TransactionDigest, error) {
	return t.digest(nil)
}

// AuthenticationMessage returns the authentication message for the transaction
func (t IncompleteCommonTx) Message() (txm TransactionMessage, err error) {
	txm.TxType = t.txType
	txm.TransactionData, err = t.self.xdrBytes() // for use in encoding signed transaction
	if err != nil {
		return
	}
	txm.Digest, err = t.digest(txm.TransactionData) // for sign transaction
	return
}

func (t IncompleteCommonTx) digest(d []byte) (_ TransactionDigest, err error) {
	hasher := NewTransactionHasher()

	networkID := GetNetworkID()
	if EnableTransactionPruning {
		if p, ok := t.self.(txMutable); ok {
			d, err = p.immutableBytes()
			if err != nil {
				return
			}
		}
	}
	if d == nil {
		d, err = t.self.xdrBytes()
		if err != nil {
			return
		}
	}

	// TransactionMessage as it's described in the
	//     https://product.spacemesh.io/#/transactions?id=signing-a-transaction
	// but we really don't need to use XDR here because it's just a concatenation of bytes string
	xdrMessage := struct {
		NetworkID [32]byte
		Type      [1]byte
		// ImmutableTransactionData for simple coin is exactly the transaction body
		//   for prunable transactions it's specifically encoded
		//   immutable body part and hasher of prunable data,
		//   so it the same for pruned and original transaction
		ImmutableTransactionData []byte
	}{networkID, [1]byte{t.txType.Value}, d}

	if _, err = xdr.Marshal(hasher, &xdrMessage); err != nil {
		return
	}
	/*
		this code uses alternative more compact encoding without XDR serializer

		_, _ = sha.Write(networkID[:])
		_, _ = sha.Write([]byte{t.txType.Value})
		// here we add original Xdr encoded transaction
		//   or Xdr encoded immutable transaction part if transaction can be pruned
		_, _ = sha.Write(d)
	*/
	return hasher.Sum(), nil
}

// Type returns transaction's type
func (t IncompleteCommonTx) Type() TransactionType {
	return t.txType
}

// Complete converts the IncompleteTransaction to the Transaction object
func (t *IncompleteCommonTx) Complete(pubKey PublicKey, signature Signature, txid TransactionID) Transaction {
	tx := t.self.complete()
	tx.txType = t.txType
	tx.origin = BytesToAddress(pubKey.Bytes())
	tx.id = txid
	tx.pubKey = pubKey
	tx.signature = signature
	return tx.self.(Transaction)
}

// decode fills the transaction object from the transaction bytes
func (t *IncompleteCommonTx) decode(data []byte, txtp TransactionType) (err error) {
	var n int
	if n, err = t.self.xdrFill(data); err != nil {
		return
	}
	if n != len(data) {
		// to protect against digest compilation attack with ED++
		//   app call/spawn txs can be vulnerable because variable call-data size
		return errBadTransactionEncodingError
	}
	t.txType = txtp
	return
}

// CommonTx is an common partial implementation of the Transaction
type CommonTx struct {
	IncompleteCommonTx
	origin    Address
	id        TransactionID
	signature Signature
	pubKey    PublicKey
}

// Origin returns transaction's origin, it implements Transaction.Origin
func (tx CommonTx) Origin() Address {
	return tx.origin
}

// Signature returns transaction's signature, it implements Transaction.Signature
func (tx CommonTx) Signature() Signature {
	return tx.signature
}

// PubKey returns transaction's public key, it implements Transaction.PubKey
func (tx CommonTx) PubKey() PublicKey {
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
	txm, err := tx.Message()
	if err != nil {
		return
	}
	return txm.Encode(tx.pubKey, tx.signature)
}

// Prune by default does nothing and returns original transaction
func (tx *CommonTx) Prune() Transaction {
	return tx.self.(Transaction)
}

// Pruned returns true if transaction is pruned, by default transaction is not prunable
func (tx *CommonTx) Pruned() bool {
	return false
}
