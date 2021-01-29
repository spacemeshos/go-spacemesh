package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

var OldCoinEdPlusType = TransactionTypeObject{
	TxOldCoinEdPlus, "TxOldCoinEdPlus", EdPlusSigningScheme, DecodeOldCoinTx,
}.New()

var OldCoinEdType = TransactionTypeObject{
	TxOldCoinEd, "TxOldCoinEd", EdSigningScheme, DecodeOldCoinTx,
}.New()

// OldCoinTx implements "Old Coin Transaction"
type OldCoinTx struct {
	AccountNonce uint64
	Recipient    Address
	GasLimit     uint64
	Fee          uint64
	Amount       uint64
}

// NewEdPlus creates a new incomplete transaction with Ed++ signing scheme
func (h OldCoinTx) NewEdPlus() IncompleteTransaction {
	tx := &incompOldCoinTx{OldCoinTxHeader{h}, IncompleteCommonTx{txType: OldCoinEdPlusType}}
	tx.self = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h OldCoinTx) NewEd() IncompleteTransaction {
	tx := &incompOldCoinTx{OldCoinTxHeader{h}, IncompleteCommonTx{txType: OldCoinEdType}}
	tx.self = tx
	return tx
}

// incompOldCoinTx implements IncompleteTransaction for a "Old Coin Transaction"
type incompOldCoinTx struct {
	OldCoinTxHeader
	IncompleteCommonTx
}

// String implements fmt.Stringer interface
func (tx incompOldCoinTx) String() string {
	return fmt.Sprintf(
		"<incomplete transaction, type: %v, recipient: %s, amount: %v, nonce: %v, gas_limit: %v, fee: %v>",
		tx.txType,
		tx.Recipient.Short(), tx.Amount, tx.AccountNonce,
		tx.GasLimit, tx.Fee)
}

// OldCoinTxHeader implements txSelf and Get* methods from IncompleteTransaction
type OldCoinTxHeader struct {
	OldCoinTx
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (h OldCoinTxHeader) Extract(out interface{}) bool {
	if p, ok := out.(*OldCoinTx); ok {
		*p = h.OldCoinTx
		return true
	}
	return false
}

// GetRecipient returns recipient address
func (h OldCoinTxHeader) GetRecipient() Address {
	return h.Recipient
}

// GetAmount returns transaction amount
func (h OldCoinTxHeader) GetAmount() uint64 {
	return h.Amount
}

// GetNonce returns transaction nonce
func (h OldCoinTxHeader) GetNonce() uint64 {
	return h.AccountNonce
}

// GetGasLimit returns transaction gas limit
func (h OldCoinTxHeader) GetGasLimit() uint64 {
	return h.GasLimit
}

// GetGasPrice returns gas price
func (h OldCoinTxHeader) GetGasPrice() uint64 {
	return 1 // TODO: there is just fee no gas price
}

// GetFee calculate transaction fee regarding gas spent
func (h OldCoinTxHeader) GetFee( /*gas*/ _ uint64) uint64 {
	return h.Fee
}

func (h OldCoinTxHeader) xdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	if _, err := xdr.Marshal(&bf, h.OldCoinTx); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

func (h *OldCoinTxHeader) xdrFill(bs []byte) (int, error) {
	return xdr.Unmarshal(bytes.NewReader(bs), &h.OldCoinTx)
}

func (h OldCoinTxHeader) complete() *CommonTx {
	tx2 := &oldCoinTx{OldCoinTxHeader: h}
	tx2.self = tx2
	return &tx2.CommonTx
}

// oldCoinTx implements TransactionInterface for "Old Coin Transaction"
type oldCoinTx struct {
	OldCoinTxHeader
	CommonTx
}

// String implements fmt.Stringer interface
func (tx oldCoinTx) String() string {
	h := tx.OldCoinTx
	return fmt.Sprintf(
		"<id: %s, type: %v, origin: %s, recipient: %s, amount: %v, nonce: %v, gas_limit: %v, fee: %v>",
		tx.ID().ShortString(), tx.Type(), tx.Origin().Short(),
		h.Recipient.Short(),
		h.Amount, h.AccountNonce, h.GasLimit, h.Fee)
}

// DecodeOldCoinTx decodes transaction bytes into "Old Coin Incomplete Transaction" object
func DecodeOldCoinTx(data []byte, txtp TransactionType) (r IncompleteTransaction, err error) {
	tx := &incompOldCoinTx{}
	tx.self = tx
	return tx, tx.decode(data, txtp)
}

// NewSignedOldCoinTx is used in TESTS ONLY to generate signed txs
func NewSignedOldCoinTx(nonce uint64, rec Address, amount, gas, fee uint64, signer Signer) (Transaction, error) {
	return SignTransaction(OldCoinTx{
		AccountNonce: nonce,
		Recipient:    rec,
		Amount:       amount,
		GasLimit:     gas,
		Fee:          fee,
	}.NewEdPlus(), signer)
}
