package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

var SimpleCoinEdPlusType = TransactionTypeObject{
	TxSimpleCoinEdPlus, "TxSimpleCoinEdPlus", EdPlusSigningScheme, DecodeSimpleCoinTx,
}.New()

var SimpleCoinEdType = TransactionTypeObject{
	TxSimpleCoinEd, "TxSimpleCoinEd", EdSigningScheme, DecodeSimpleCoinTx,
}.New()

// SimpleCoinTx implements "Simple Coin Transaction"
type SimpleCoinTx struct {
	TTL       uint32
	Nonce     byte
	Recipient Address
	Amount    uint64
	GasLimit  uint64
	GasPrice  uint64
}

type xdrSimpleCoinTx struct {
	TTL       uint32
	Nonce     [1]byte
	Recipient Address
	Amount    uint64
	GasLimit  uint64
	GasPrice  uint64
}

// NewEdPlus creates a new incomplete transaction with Ed++ signing scheme
func (h SimpleCoinTx) NewEdPlus() IncompleteTransaction {
	tx := &incompSimpleCoinTx{SimpleCoinTxHeader{h}, IncompleteCommonTx{txType: SimpleCoinEdPlusType}}
	tx.self = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h SimpleCoinTx) NewEd() IncompleteTransaction {
	tx := &incompSimpleCoinTx{SimpleCoinTxHeader{h}, IncompleteCommonTx{txType: SimpleCoinEdType}}
	tx.self = tx
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

// SimpleCoinTxHeader implements txSelf and Get* methods from IncompleteTransaction
type SimpleCoinTxHeader struct {
	SimpleCoinTx
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (h SimpleCoinTxHeader) Extract(out interface{}) bool {
	if p, ok := out.(*SimpleCoinTx); ok {
		*p = h.SimpleCoinTx
		return true
	}
	return false
}

// GetRecipient returns recipient address
func (h SimpleCoinTxHeader) GetRecipient() Address {
	return h.Recipient
}

// GetAmount returns transaction amount
func (h SimpleCoinTxHeader) GetAmount() uint64 {
	return h.Amount
}

// GetNonce returns transaction nonce
func (h SimpleCoinTxHeader) GetNonce() uint64 {
	// TODO: nonce processing
	return uint64(h.Nonce)
}

// GetGasLimit returns transaction gas limit
func (h SimpleCoinTxHeader) GetGasLimit() uint64 {
	return h.GasLimit
}

// GetGasPrice returns gas price
func (h SimpleCoinTxHeader) GetGasPrice() uint64 {
	return h.GasPrice
}

// GetFee calculate transaction fee regarding gas spent
func (h SimpleCoinTxHeader) GetFee(gas uint64) uint64 {
	return h.GasPrice * gas
}

func (h SimpleCoinTxHeader) xdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	d := xdrSimpleCoinTx{
		h.TTL,
		[1]byte{h.Nonce},
		h.Recipient,
		h.Amount,
		h.GasLimit,
		h.GasPrice,
	}
	if _, err := xdr.Marshal(&bf, &d); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

func (h *SimpleCoinTxHeader) xdrFill(bs []byte) (n int, err error) {
	d := xdrSimpleCoinTx{}
	n, err = xdr.Unmarshal(bytes.NewReader(bs), &d)
	h.SimpleCoinTx = SimpleCoinTx{
		d.TTL,
		d.Nonce[0],
		d.Recipient,
		d.Amount,
		d.GasLimit,
		d.GasPrice,
	}
	return
}

func (h SimpleCoinTxHeader) complete() *CommonTx {
	tx2 := &simpleCoinTx{SimpleCoinTxHeader: h}
	tx2.self = tx2
	return &tx2.CommonTx
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
func DecodeSimpleCoinTx(data []byte, txtp TransactionType) (r IncompleteTransaction, err error) {
	tx := &simpleCoinTx{}
	tx.self = tx
	return tx, tx.decode(data, txtp)
}
