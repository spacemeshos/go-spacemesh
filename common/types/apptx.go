package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

// CallAppTx implements "Call App Transaction"
type CallAppTx struct {
	TTL        uint32  // TTL TODO: update
	Nonce      byte    // Nonce TODO: update
	AppAddress Address // AppAddress Recipient App Address to Call
	Amount     uint64  // Amount of the transaction
	GasLimit   uint64  // GasLimit for the transaction
	GasPrice   uint64  // GasPrice for the transaction
	CallData   []byte  // CallData an additional data
}

// NewEdPlus creates a new incomplete transaction with Ed++ signing scheme
func (h CallAppTx) NewEdPlus() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{h}, IncompleteCommonTx{txType: TxCallAppEdPlus}}
	tx.marshal = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h CallAppTx) NewEd() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{h}, IncompleteCommonTx{txType: TxCallAppEd}}
	tx.marshal = tx
	return tx
}

// incompCallAppTx implements IncompleteTransaction for a "Call App Transaction"
type incompCallAppTx struct {
	CallAppTxHeader
	IncompleteCommonTx
}

// String implements fmt.Stringer interface
func (tx incompCallAppTx) String() string {
	return fmt.Sprintf(
		"<incomplete transaction, type: %v, app: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.txType,
		tx.AppAddress.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (tx incompCallAppTx) Extract(out interface{}) bool {
	return tx.extract(out, tx.txType)
}

// CallAppTxHeader implements xdrMarshal and Get* methods from IncompleteTransaction
type CallAppTxHeader struct {
	CallAppTx
}

// XdrBytes implements xdrMarshal.XdrBytes
func (tx CallAppTxHeader) XdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	if _, err := xdr.Marshal(&bf, &tx.CallAppTx); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

// XdrFill implements xdrMarshal.XdrFill
func (tx *CallAppTxHeader) XdrFill(bs []byte) (err error) {
	_, err = xdr.Unmarshal(bytes.NewReader(bs), &tx.CallAppTx)
	return
}

func (tx CallAppTxHeader) extract(out interface{}, tt TransactionType) bool {
	if p, ok := out.(*CallAppTx); ok && (tt == TxCallAppEd || tt == TxCallAppEdPlus) {
		*p = tx.CallAppTx
		return true
	}
	if p, ok := out.(*SpawnAppTx); ok && (tt == TxSpawnAppEd || tt == TxSpawnAppEdPlus) {
		*p = SpawnAppTx(tx.CallAppTx)
		return true
	}
	return false
}

// GetRecipient returns recipient address
func (tx CallAppTxHeader) GetRecipient() Address {
	return tx.AppAddress
}

// GetAmount returns transaction amount
func (tx CallAppTxHeader) GetAmount() uint64 {
	return tx.Amount
}

// GetNonce returns transaction nonce
func (tx CallAppTxHeader) GetNonce() uint64 {
	// TODO: nonce processing
	return uint64(tx.Nonce)
}

// GetGasLimit returns transaction gas limit
func (tx CallAppTxHeader) GetGasLimit() uint64 {
	return tx.GasLimit
}

// GetGasPrice returns gas price
func (tx CallAppTxHeader) GetGasPrice() uint64 {
	return tx.GasPrice
}

// GetFee calculate transaction fee regarding gas spent
func (tx CallAppTxHeader) GetFee(gas uint64) uint64 {
	return tx.GasPrice * gas
}

// SpawnAppTx implements "Spawn App Transaction"
type SpawnAppTx CallAppTx

// NewEdPlus creates a new incomplete transaction with Ed++ signing scheme
func (h SpawnAppTx) NewEdPlus() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{CallAppTx(h)}, IncompleteCommonTx{txType: TxSpawnAppEdPlus}}
	tx.marshal = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h SpawnAppTx) NewEd() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{CallAppTx(h)}, IncompleteCommonTx{txType: TxSpawnAppEd}}
	tx.marshal = tx
	return tx
}

// callAppTx implements TransactionInterface for "Call App Transaction" and "Spawn App Transaction"
type callAppTx struct {
	CallAppTxHeader
	CommonTx
}

// DecodeCallAppTx decodes transaction bytes into "Call App Transaction" object
func DecodeCallAppTx(data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (r Transaction, err error) {
	tx := &callAppTx{}
	return tx, tx.decode(tx, data, signature, pubKey, txid, txtp)
}

// DecodeSpawnAppTx decodes transaction bytes into "Spawn App Transaction" object
func DecodeSpawnAppTx(data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (r Transaction, err error) {
	tx := &callAppTx{}
	return tx, tx.decode(tx, data, signature, pubKey, txid, txtp)
}

// String implements fmt.Stringer interface
func (tx callAppTx) String() string {
	return fmt.Sprintf(
		"<id: %s, type: %v, origin: %s, app: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.ID().ShortString(), tx.txType, tx.Origin().Short(),
		tx.AppAddress.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (tx callAppTx) Extract(out interface{}) bool {
	return tx.extract(out, tx.txType)
}
