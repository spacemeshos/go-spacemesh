package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
)

type CallAppTx struct {
	TTL        uint32
	Nonce      byte
	AppAddress Address
	Amount     uint64
	GasLimit   uint64
	GasPrice   uint64
	CallData   []byte
}

func (h CallAppTx) NewEdPlus() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{h}, IncompleteCommonTx{txType: TxCallAppEdPlus}}
	tx.marshal = tx
	return tx
}

func (h CallAppTx) NewEd() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{h}, IncompleteCommonTx{txType: TxCallAppEd}}
	tx.marshal = tx
	return tx
}

type incompCallAppTx struct {
	CallAppTxHeader
	IncompleteCommonTx
}

func (tx incompCallAppTx) String() string {
	return fmt.Sprintf(
		"<incomplete transaction, type: %v, app: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.txType,
		tx.AppAddress.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}

func (tx incompCallAppTx) Extract(out interface{}) bool {
	return tx.extract(out, tx.txType)
}

type CallAppTxHeader struct {
	CallAppTx
}

func (tx CallAppTxHeader) XdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	if _, err := xdr.Marshal(&bf, &tx.CallAppTx); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

func (h *CallAppTxHeader) XdrFill(bs []byte) (err error) {
	_, err = xdr.Unmarshal(bytes.NewReader(bs), &h.CallAppTx)
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

func (h CallAppTxHeader) GetRecipient() Address {
	return h.AppAddress
}

func (h CallAppTxHeader) GetAmount() uint64 {
	return h.Amount
}

func (h CallAppTxHeader) GetNonce() uint64 {
	// TODO: nonce processing
	return uint64(h.Nonce)
}

func (h CallAppTxHeader) GetGasLimit() uint64 {
	return h.GasLimit
}

func (h CallAppTxHeader) GetGasPrice() uint64 {
	return h.GasPrice
}

func (h CallAppTxHeader) GetFee(gas uint64) uint64 {
	return h.GasPrice * gas
}

type SpawnAppTx CallAppTx

func (h SpawnAppTx) NewEdPlus() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{CallAppTx(h)}, IncompleteCommonTx{txType: TxSpawnAppEdPlus}}
	tx.marshal = tx
	return tx
}

func (h SpawnAppTx) NewEd() IncompleteTransaction {
	tx := &incompCallAppTx{CallAppTxHeader{CallAppTx(h)}, IncompleteCommonTx{txType: TxSpawnAppEd}}
	tx.marshal = tx
	return tx
}

type callAppTx struct {
	CallAppTxHeader
	CommonTx
}

func DecodeCallAppTx(data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (r Transaction, err error) {
	tx := &callAppTx{}
	return tx, tx.decode(tx, data, signature, pubKey, txid, txtp)
}

func DecodeSpawnAppTx(data []byte, signature TxSignature, pubKey TxPublicKey, txid TransactionID, txtp TransactionType) (r Transaction, err error) {
	tx := &callAppTx{}
	return tx, tx.decode(tx, data, signature, pubKey, txid, txtp)
}

func (tx callAppTx) Extract(out interface{}) bool {
	return tx.extract(out, tx.txType)
}

func (tx callAppTx) String() string {
	return fmt.Sprintf(
		"<id: %s, type: %v, origin: %s, app: %s, ttl: %d, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.ID().ShortString(), tx.txType, tx.Origin().Short(),
		tx.AppAddress.Short(), tx.TTL, tx.Amount, tx.Nonce,
		tx.GasLimit, tx.GasPrice)
}
