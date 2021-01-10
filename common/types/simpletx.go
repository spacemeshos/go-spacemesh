package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

type SimpleCoinTx struct {
	TTL       uint32
	Nonce     byte
	Recipient Address
	Amount    uint64
	GasLimit  uint64
	GasPrice  uint64
}

func (h SimpleCoinTx) NewEdPlus() IncompleteTransaction {
	tx := &incompSimpleCoinTx{SimpleCoinTxHeader{h}, IncompleteCommonTx{txType: TxSimpleCoinEdPlus}}
	tx.marshal = tx
	return tx
}

func (h SimpleCoinTx) NewEd() IncompleteTransaction {
	tx := &incompSimpleCoinTx{SimpleCoinTxHeader{h}, IncompleteCommonTx{txType: TxSimpleCoinEd}}
	tx.marshal = tx
	return tx
}

type incompSimpleCoinTx struct {
	SimpleCoinTxHeader
	IncompleteCommonTx
}

func (tx incompSimpleCoinTx) String() string {
	return fmt.Sprintf(
		"<incomplete transaction, type: %v, recipient: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.txType,
		tx.Recipient.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

type SimpleCoinTxHeader struct {
	SimpleCoinTx
}

func (tx SimpleCoinTxHeader) Extract(out interface{}) bool {
	if p, ok := out.(*SimpleCoinTx); ok {
		*p = tx.SimpleCoinTx
		return true
	}
	return false
}

func (h SimpleCoinTxHeader) GetRecipient() Address {
	return h.Recipient
}

func (h SimpleCoinTxHeader) GetAmount() uint64 {
	return h.Amount
}

func (h SimpleCoinTxHeader) GetNonce() uint64 {
	// TODO: nonce processing
	return uint64(h.Nonce)
}

func (h SimpleCoinTxHeader) GetGasLimit() uint64 {
	return h.GasLimit
}

func (h SimpleCoinTxHeader) GetGasPrice() uint64 {
	return h.GasPrice
}

func (h SimpleCoinTxHeader) GetFee(gas uint64) uint64 {
	return h.GasPrice * gas
}

func (h SimpleCoinTxHeader) XdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	if _, err := xdr.Marshal(&bf, &h); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

func (h *SimpleCoinTxHeader) XdrFill(bs []byte) (err error) {
	_, err = xdr.Unmarshal(bytes.NewReader(bs), &h.SimpleCoinTx)
	return
}

type simpleCoinTx struct {
	SimpleCoinTxHeader
	CommonTx
}

func (tx simpleCoinTx) String() string {
	return fmt.Sprintf(
		"<id: %s, type: %v, origin: %s, recipient: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.ID().ShortString(), tx.txType, tx.Origin().Short(),
		tx.Recipient.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

func DecodeSimpleCoinTx(data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (r Transaction, err error) {
	tx := &simpleCoinTx{}
	return tx, tx.decode(tx, data, signature, pubKey, txid, txtp)
}
