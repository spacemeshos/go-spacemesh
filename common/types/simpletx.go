package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

// SimpleCoinTx implements "Simple Coin Transaction"
type SimpleCoinTx struct {
	TTL       uint32
	Nonce     byte
	Recipient Address
	Amount    uint64
	GasLimit  uint64
	GasPrice  uint64
}

// NewEdPlus creates a new incomplete transaction with Ed++ signing scheme
func (h SimpleCoinTx) NewEdPlus() IncompleteTransaction {
	tx := &incompSimpleCoinTx{SimpleCoinTxHeader{h}, IncompleteCommonTx{txType: TxSimpleCoinEdPlus}}
	tx.marshal = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h SimpleCoinTx) NewEd() IncompleteTransaction {
	tx := &incompSimpleCoinTx{SimpleCoinTxHeader{h}, IncompleteCommonTx{txType: TxSimpleCoinEd}}
	tx.marshal = tx
	return tx
}

// incompSimpleCoinTx implements IncompleteTransaction for a "Simple Coin Transaction"
type incompSimpleCoinTx struct {
	SimpleCoinTxHeader
	IncompleteCommonTx
}

// String implements fmt.Stringer interface
func (tx incompSimpleCoinTx) String() string {
	return fmt.Sprintf(
		"<incomplete transaction, type: %v, recipient: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.txType,
		tx.Recipient.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

// SimpleCoinTxHeader implements xdrMarshal and Get* methods from IncompleteTransaction
type SimpleCoinTxHeader struct {
	SimpleCoinTx
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (tx SimpleCoinTxHeader) Extract(out interface{}) bool {
	if p, ok := out.(*SimpleCoinTx); ok {
		*p = tx.SimpleCoinTx
		return true
	}
	return false
}

// GetRecipient returns recipient address
func (tx SimpleCoinTxHeader) GetRecipient() Address {
	return tx.Recipient
}

// GetAmount returns transaction amount
func (tx SimpleCoinTxHeader) GetAmount() uint64 {
	return tx.Amount
}

// GetNonce returns transaction nonce
func (tx SimpleCoinTxHeader) GetNonce() uint64 {
	// TODO: nonce processing
	return uint64(tx.Nonce)
}

// GetGasLimit returns transaction gas limit
func (tx SimpleCoinTxHeader) GetGasLimit() uint64 {
	return tx.GasLimit
}

// GetGasPrice returns gas price
func (tx SimpleCoinTxHeader) GetGasPrice() uint64 {
	return tx.GasPrice
}

// GetFee calculate transaction fee regarding gas spent
func (tx SimpleCoinTxHeader) GetFee(gas uint64) uint64 {
	return tx.GasPrice * gas
}

// XdrBytes implements xdrMarshal.XdrBytes
func (tx SimpleCoinTxHeader) XdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	if _, err := xdr.Marshal(&bf, &tx); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

// XdrFill implements xdrMarshal.XdrFill
func (tx *SimpleCoinTxHeader) XdrFill(bs []byte) (int, error) {
	return xdr.Unmarshal(bytes.NewReader(bs), &tx.SimpleCoinTx)
}

// simpleCoinTx implements TransactionInterface for "Simple Coin Transaction"
type simpleCoinTx struct {
	SimpleCoinTxHeader
	CommonTx
}

// String implements fmt.Stringer interface
func (tx simpleCoinTx) String() string {
	return fmt.Sprintf(
		"<id: %s, type: %v, origin: %s, recipient: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.ID().ShortString(), tx.txType, tx.Origin().Short(),
		tx.Recipient.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

// DecodeSimpleCoinTx decodes transaction bytes into "Simple Coin Transaction" object
func DecodeSimpleCoinTx(data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (r Transaction, err error) {
	tx := &simpleCoinTx{}
	return tx, tx.decode(tx, data, signature, pubKey, txid, txtp)
}
