package types

import (
	"bytes"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type OldCoinTx struct {
	AccountNonce uint64
	Recipient    Address
	GasLimit     uint64
	Fee          uint64
	Amount       uint64
}

func (h OldCoinTx) NewEdPlus() IncompleteTransaction {
	tx := &incompOldCoinTx{OldCoinTxHeader{h}, IncompleteCommonTx{txType: TxOldCoinEdPlus}}
	tx.marshal = tx
	return tx
}

func (h OldCoinTx) NewEd() IncompleteTransaction {
	tx := &incompOldCoinTx{OldCoinTxHeader{h}, IncompleteCommonTx{txType: TxOldCoinEd}}
	tx.marshal = tx
	return tx
}

type incompOldCoinTx struct {
	OldCoinTxHeader
	IncompleteCommonTx
}

func (tx incompOldCoinTx) String() string {
	return fmt.Sprintf(
		"<incomplete transaction, type: %v, recipient: %s, amount: %v, nonce: %v, gas_limit: %v, fee: %v>",
		tx.txType,
		tx.Recipient.Short(), tx.Amount, tx.AccountNonce,
		tx.GasLimit, tx.Fee)
}

type OldCoinTxHeader struct {
	OldCoinTx
}

func (tx OldCoinTxHeader) Extract(out interface{}) bool {
	if p, ok := out.(*OldCoinTx); ok {
		*p = tx.OldCoinTx
		return true
	}
	return false
}

func (h OldCoinTxHeader) GetRecipient() Address {
	return h.Recipient
}

func (h OldCoinTxHeader) GetAmount() uint64 {
	return h.Amount
}

func (h OldCoinTxHeader) GetNonce() uint64 {
	return h.AccountNonce
}

func (h OldCoinTxHeader) GetGasLimit() uint64 {
	return h.GasLimit
}

func (h OldCoinTxHeader) GetGasPrice() uint64 {
	return 1 // TODO: there is just fee no gas price
}

func (h OldCoinTxHeader) GetFee( /*gas*/ _ uint64) uint64 {
	return h.Fee
}

func (h OldCoinTxHeader) XdrBytes() ([]byte, error) {
	bf := bytes.Buffer{}
	if _, err := xdr.Marshal(&bf, h.OldCoinTx); err != nil {
		return nil, err
	}
	return bf.Bytes(), nil
}

func (h *OldCoinTxHeader) XdrFill(bs []byte) (err error) {
	_, err = xdr.Unmarshal(bytes.NewReader(bs), &h.OldCoinTx)
	return
}

type oldCoinTx struct {
	OldCoinTxHeader
	CommonTx
}

func (tx oldCoinTx) String() string {
	h := tx.OldCoinTx
	return fmt.Sprintf(
		"<id: %s, type: %v, origin: %s, recipient: %s, amount: %v, nonce: %v, gas_limit: %v, gas_price: %v>",
		tx.ID().ShortString(), tx.Type(), tx.Origin().Short(),
		h.Recipient, h.Amount, h.AccountNonce, h.GasLimit, h.Fee)
}

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
