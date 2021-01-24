package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/signing"
)

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
	tx := &incompOldCoinTx{OldCoinTxHeader{h}, IncompleteCommonTx{txType: TxOldCoinEdPlus}}
	tx.marshal = tx
	return tx
}

// NewEd creates a new incomplete transaction with Ed signing scheme
func (h OldCoinTx) NewEd() IncompleteTransaction {
	tx := &incompOldCoinTx{OldCoinTxHeader{h}, IncompleteCommonTx{txType: TxOldCoinEd}}
	tx.marshal = tx
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

// OldCoinTxHeader implements xdrMarshal and Get* methods from IncompleteTransaction
type OldCoinTxHeader struct {
	OldCoinTx
}

// Extract implements IncompleteTransaction.Extract to extract internal transaction structure
func (tx OldCoinTxHeader) Extract(out interface{}) bool {
	if p, ok := out.(*OldCoinTx); ok {
		*p = tx.OldCoinTx
		return true
	}
	return false
}

// GetRecipient returns recipient address
func (tx OldCoinTxHeader) GetRecipient() Address {
	return tx.Recipient
}

// GetAmount returns transaction amount
func (tx OldCoinTxHeader) GetAmount() uint64 {
	return tx.Amount
}

// GetNonce returns transaction nonce
func (tx OldCoinTxHeader) GetNonce() uint64 {
	return tx.AccountNonce
}

// GetGasLimit returns transaction gas limit
func (tx OldCoinTxHeader) GetGasLimit() uint64 {
	return tx.GasLimit
}

// GetGasPrice returns gas price
func (tx OldCoinTxHeader) GetGasPrice() uint64 {
	return 1 // TODO: there is just fee no gas price
}

// GetFee calculate transaction fee regarding gas spent
func (tx OldCoinTxHeader) GetFee( /*gas*/ _ uint64) uint64 {
	return tx.Fee
}

// XdrBytes implements xdrMarshal.XdrBytes
func (tx OldCoinTxHeader) XdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	if _, err := xdr.Marshal(&bf, tx.OldCoinTx); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

// XdrFill implements xdrMarshal.XdrFill
func (tx *OldCoinTxHeader) XdrFill(bs []byte) (int, error) {
	return xdr.Unmarshal(bytes.NewReader(bs), &tx.OldCoinTx)
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

// DecodeOldCoinTx decodes transaction bytes into "Old Coin Transaction" object
func DecodeOldCoinTx(data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (r Transaction, err error) {
	tx := &oldCoinTx{}
	return tx, tx.decode(tx, data, signature, pubKey, txid, txtp)
}

// NewSignedOldCoinTx is used in TESTS ONLY to generate signed txs
func NewSignedOldCoinTx(nonce uint64, rec Address, amount, gas, fee uint64, signer *signing.EdSigner) (Transaction, error) {
	return SignTransaction(OldCoinTx{
		AccountNonce: nonce,
		Recipient:    rec,
		Amount:       amount,
		GasLimit:     gas,
		Fee:          fee,
	}.NewEdPlus(), signer)
}
